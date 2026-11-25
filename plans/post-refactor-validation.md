# SandPod — Post-refactor validation findings (2026-11-25)

> Nguồn: validation thực tế sau refactor `75a24ed` (Go tests, pytest, panel-api
> gọi binary thật, CLI trên host, parity với script bash cũ).
> Quy ước: `- [ ]` chưa làm, `- [x]` done.

## Part 1 — Migrate pilot 001/002 sang image `node-shared`

Hiện trạng host `2026-11-25`:

* `~/config/nginx.conf` của pilot chỉ map `php82/83/84`; `php85/86` rơi về
  default 8.3.
* Image `:pilot` không có `php86-fpm` (và helper `sandpod-runtime-reload` cũ).
* Quadlet đã cài trong `~/.config/containers/systemd/` thiếu mount
  `/opt/sandpod/node|python`, thiếu `20-resources.conf` và các cap
  `MemoryHigh/Max/SwapMax/TasksMax/CPUWeight/IOWeight/--cpus/--blkio-weight`.
* `sandpodctl health-check 18001` fail ở `php85/86` (HTTP 200 nhưng body 8.3).

- [ ] Migrate `001`/`002` sang `localhost/sandpod/customer-runtime:node-shared`
  (registry + Quadlet instance), cài lại template hiện tại + drop-in
  `20-resources.conf` (1 CPU / 1 GiB / swap 0 / 512 pids / weights).
- [ ] Đồng bộ `~/config/nginx.conf` từ `modules/customer-container/config/nginx.conf`
  (map đủ `php82..php86`), `nginx -t` rồi reload trong container.
- [ ] Verify: `sandpodctl health-check 18001` và `... 18002` đều pass 5/5 minor,
  `sandpodctl resource-report` thấy cap mới, container vẫn rootless/loopback-only.
- [ ] Cập nhật `AGENTS.md`/README pilot nếu quyết định bỏ hẳn tag `:pilot`.

`scope: in-scope — vận hành host (customer-provisioning + customer-container); quyết định migrate thuộc operator/panel.`

## Part 2 — Chuẩn hoá format `/etc/projid` + `/etc/projects`

Hiện trạng: repo ghi `/etc/projid` = `<uid>:<user>`, `/etc/projects` =
`<user>:<home>`, nhưng `xfs_quota(8)`/`projid(5)`/`projects(5)` quy định:

* `/etc/projid` = `<name>:<id>` (vd `svc-c001:20001`)
* `/etc/projects` = `<id>:<path>` (vd `20001:/home/svc-c001`)

Hệ quả: quota vẫn enforce bằng numeric ID, nhưng `xfs_quota report -p` không
translate được tên (in `#20001` thay vì `svc-c001`); parser
`resource-report` phải xử lý cả layout không tên (đã fix `2026-11-25`).

- [ ] Sửa `internal/storage.SetCustomerQuota` ghi đúng chuẩn `name:id` /
  `id:path`; upsert idempotent, migrate entry cũ (`<uid>:<user>` /
  `<user>:<home>`) thay vì để trùng.
- [ ] Sửa `internal/storage.QuotaReport` + `ReapplyQuotas` đọc `/etc/projid`
  theo `name:id` (lấy id ở vế sau), không phụ thuộc thứ tự cũ.
- [ ] Cập nhật `modules/host-storage/README.md` (Conventions) + test Go cho
  parse/migrate 2 format.
- [ ] Verify trên host: sau `sandpodctl set-customer-quota svc-c00X`,
  `xfs_quota -x -P /etc/projid -D /etc/projects -c 'report -p -b -h' /` in
  `svc-c00X` (không còn `#20001`), limit vẫn giữ; `quota-report` và
  `resource-report --json` khớp số.
- [ ] Chạy `sandpodctl reapply-quotas` sau migration và health-check lại quota.

`scope: in-scope — host-storage; migration chạm file hệ thống nên làm theo batch + backup /etc/projid,/etc/projects.`
