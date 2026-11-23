# SandPod — Kế hoạch Pha 2 + 3

> Baseline: `2026-11-20`. Pha 0/1 vẫn là chặn production, Pha 2+3 dưới đây làm sau trên nhánh/image mới, không đụng pilot `001`.

## Quy tắc (bắt buộc)

1. Không đụng host: không ssh/sudo/thi hành trên host, không provision/rebuild/restart thật.
2. Không verify E2E: chỉ verify mức repo (`bash -n`, đối chiếu `grep`, test logic trích rời với fixture trong `/tmp`).
3. Mọi task trong plan phải xong được ở mức repo; việc cần host/E2E không cho vào plan khi chưa được yêu cầu explicitly.
4. Không xóa bản PHP nào — giữ đủ bản cho KH lựa (quyết định `2026-11-20`).
5. Đụng contract panel (mount/port/program/TSV) phải bump Spec trong `docs/control-panel.md` trước khi code.

## Pha 2: Runtime & Scale

- [ ] **2.1 Shared Node/Python** — host quản lý, container mount RO, image gọn dần — due: `2026-12-04`
  - [x] `install-node-runtimes.sh` + `install-python-runtimes.sh` (host, label SELinux, verify `node --version`) — done: `2026-11-20`
  - [x] `Containerfile` tạo placeholder `/opt/sandpod/node`, `/opt/sandpod/python` + symlink compat — done: `2026-11-20` (node placeholder + mount; python interim dùng host RPM)
  - [x] `quadlet/customer@.container` thêm 2 mount RO — done: `2026-11-20` (node mount xong; python mount toàn `/opt/sandpod` chờ prefix build)
  - [x] `reload-customer-app.sh` (`supervisorctl restart app-<domain>`) + docs — done: `2026-11-20`
- [x] **2.2 Vòng đời PHP (giữ đủ bản cho KH lựa)** — done: `2026-11-20`
  - [x] Cảnh báo EOL 8.2 trong `add-site.sh`/`set-php-version.sh` — done: `2026-11-20` (`eol_warn_for_minor` trong `lib.sh`, warn stderr không chặn panel)
  - [x] Thêm 8.6 song song (php-fpm conf + supervisord + nginx map + reload/rollout) — done: `2026-11-20` (FPM `127.0.0.1:9086`, giữ nguyên 8.2-8.5, không xóa bản nào)
  - [x] Giữ 8.2 cho KH lựa chọn (bỏ kế hoạch xóa) — quyết định: `2026-11-20`
- [x] **2.5 Khóa CPU/RAM/IO theo customer** — done: `2026-11-23` (repo-only)
  - [x] template `customer@.container`: podman-level (`Memory=1g`, `PidsLimit=512`, `PodmanArgs --memory-swap=1g --cpus=1.0 --blkio-weight=100 --memory-swappiness=0`) + systemd-level (`CPUQuota=100%`, `CPUWeight=100`, `MemoryHigh=900M`, `MemoryMax=1G`, `MemorySwapMax=0`, `TasksMax=1024`, `IOWeight=100`)
  - [x] provision `--cpus/--memory/--io-weight` (defaults = template), chốt theo instance vào `20-resources.conf` (re-run để đổi; khách cũ re-run để nhận)
  - [x] `resource-report.sh` in dòng limits mới (swap 0, TasksMax, IOWeight)
- [x] **2.4 Provisioning cứng hơn** — done: `2026-11-23` (repo-only, không host/E2E)
  - [x] `flock` registry (`<registry>.lock` cho provision + remove `--delete-registry-entry`, re-validate ports dưới lock)
  - [x] allocator port: fix ssh grep `\t<port>$` → `\t<port>\t`, dùng `$REGISTRY`, `--registry` + `SANDPOD_USED_PORTS` cho fixture test
  - [x] quota inode: `set-customer-quota.sh --hard-inodes/--soft-inodes` (`ihard/isoft`), provision passthrough
  - [x] `resource-report` đọc `xfs_quota` (cột DISK used/soft/hard + cột INODES, join exact-match theo project name hoặc numeric id, `--json` cho panel, `--xfs-report-file/--xfs-inode-report-file` cho fixture, exit 1/2 đúng quy ước)

## Pha 3: Panel contract

- [x] **3.1 Spec v2 (`docs/control-panel.md`)** — done: `2026-11-20` (đóng băng node mount, exit codes 0/1/2, TSV schemas, EOL warn policy; `list-sites.sh --json` cho panel)

## Thứ tự thực hiện (bắt buộc)

1. `3.1` trước `2.x` (đóng băng tên mount/port/program) — đã xong.
2. `2.1` trên image tag mới (`customer-runtime:node-shared`), giữ tag `pilot` cho customer cũ.
