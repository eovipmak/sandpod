# SandPod — Post-refactor validation findings (2026-11-25)

> Nguồn: validation thực tế sau refactor `75a24ed` (Go tests, pytest, panel-api
> gọi binary thật, CLI trên host, parity với script bash cũ).
> Quy ước: `- [ ]` chưa làm, `- [x]` done.

## Part 1 — Migrate pilot 001/002 sang image `node-shared` ✅ 2026-11-25

Hiện trạng host trước migration:

* `~/config/nginx.conf` của pilot chỉ map `php82/83/84`; `php85/86` rơi về
  default 8.3.
* Image `:pilot` không có `php86-fpm` (và helper `sandpod-runtime-reload` cũ).
* Quadlet đã cài trong `~/.config/containers/systemd/` thiếu mount
  `/opt/sandpod/node|python`, thiếu `20-resources.conf` và các cap
  `MemoryHigh/Max/SwapMax/TasksMax/CPUWeight/IOWeight/--cpus`.
* `sandpodctl health-check 18001` fail ở `php85/86` (HTTP 200 nhưng body 8.3).

- [x] Migrate `001`/`002` sang `localhost/sandpod/customer-runtime:node-shared`
  (registry + Quadlet instance), cài lại template hiện tại + drop-in
  `20-resources.conf` (1 CPU / 1 GiB / swap 0 / 512 pids / weights).
- [x] Đồng bộ `~/config/nginx.conf` từ `modules/customer-container/config/nginx.conf`
  (map đủ `php82..php86`, `include sites/*.conf`), backup `.bak.<ts>`,
  `nginx -t` rồi reload trong container.
- [x] Verify: `sandpodctl health-check 18001` và `... 18002` đều pass 5/5 minor,
  `sandpodctl resource-report` thấy cap mới (`mem=1GiB`, `pids=512`,
  `cpu=1.0`, `swap=0`, systemd `TasksMax=1024`), container vẫn rootless
  (`userns=private`) và loopback-only (`127.0.0.1:1800x/2220x`).
- [x] Dữ liệu pilot `001` chuyển `~/data/public` → `~/data/public_html` cho
  khớp template `$site_root` (002 đã đúng từ trước).
- [x] Bỏ hẳn chính sách giữ tag `:pilot`: xoá image khỏi root + storage từng
  account, registry/docs chỉ còn `:node-shared`; `AGENTS.md`,
  customer-container/provisioning/shared-runtimes README và Spec đã cập nhật.

`scope: in-scope — vận hành host (customer-provisioning + customer-container); quyết định migrate thuộc operator/panel.`

## Part 2 — Chuẩn hoá format `/etc/projid` + `/etc/projects` ✅ 2026-11-25

Hiện trạng: repo ghi `/etc/projid` = `<uid>:<user>`, `/etc/projects` =
`<user>:<home>`, nhưng `xfs_quota(8)`/`projid(5)`/`projects(5)` quy định:

* `/etc/projid` = `<name>:<id>` (vd `svc-c001:20001`)
* `/etc/projects` = `<id>:<path>` (vd `20001:/home/svc-c001`)

Hệ quả: quota vẫn enforce bằng numeric ID, nhưng `xfs_quota report -p` không
translate được tên (in `#20001` thay vì `svc-c001`).

- [x] `internal/storage.SetCustomerQuota` ghi đúng chuẩn `name:id` / `id:path`;
  `mergeCanonical` idempotent, migrate entry cũ (`<uid>:<user>` /
  `<user>:<home>`) thay vì để trùng; `@/etc/projid` + `/etc/projects` được
  backup trước migration.
- [x] `internal/storage.QuotaReport` + `ReapplyQuotas` đọc `/etc/projid` qua
  `parseProjidLine` (nhận cả 2 layout); `QuotaReport` khớp cả tên project lẫn
  `#<id>` để report hoạt động trước/sau migration.
- [x] `modules/host-storage/README.md` (Conventions) + `storage_test.go`:
  parse 2 format, migrate legacy, idempotent, xoá path cũ/entry trùng.
- [x] Verify trên host: `xfs_quota -x -P /etc/projid -D /etc/projects -c
  'report -p -b -h' /` in `svc-c001`/`svc-c002` (không còn `#20001`);
  `quota-report` và `resource-report --json` khớp số (2.6G/8G/10G tại thời
  điểm verify).
- [x] Chạy `sandpodctl reapply-quotas` sau migration; quota vẫn 8G/10G và
  health-check 001/002 vẫn pass 5/5.

`scope: in-scope — host-storage; migration chạm file hệ thống nên làm theo batch + backup /etc/projid,/etc/projects.`

## Part 3 — Bugs phát hiện khi migrate thật ✅ 2026-11-25

- [x] `platform.AsUser` không set `Dir`: `runuser` giữ cwd của root
  (`/root/customer.container`) nên account customer `chdir` fail
  (`runuser: cannot chdir ... Permission denied`) → mọi lệnh `podman`/
  `systemctl --user` qua `sandpodctl` hỏng khi operator đứng trong home root.
  Fix: `c.Dir = home` sau khi resolve account.
- [x] `--memory-swappiness=0` làm `podman run` fail `status=126`
  (`crun: cannot set memory swappiness with cgroupv2`) → bỏ khỏi template +
  `20-resources.conf`; `MemorySwapMax=0` đã đủ tắt swap.
- [x] `--blkio-weight` làm `podman run` fail `status=127`
  (`crun: open io.weight ... No such file or directory`) vì rootless user
  slice không delegate io controller → bỏ flag podman; giữ systemd
  `IOWeight=100` (best-effort, unit vẫn start).
- [x] `provision-customer` in "Failed to enable unit ... transient or
  generated" cho Quadlet — vô hại (unit generated) nhưng dễ gây hiểu nhầm.
  Fix: chạy `systemctl --user enable` best-effort + nuốt output; linger +
  `default.target` vẫn là cơ chế start thực tế.

`scope: in-scope — customer-container + sandpodctl runtime; các fix đã verify bằng start/restart thật trên host.`
