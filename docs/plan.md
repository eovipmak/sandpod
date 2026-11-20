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
- [ ] **2.4 Provisioning cứng hơn** — `flock` registry, allocator port, quota inode, `resource-report` đọc `xfs_quota` — due: `2026-12-27`

## Pha 3: Panel contract

- [x] **3.1 Spec v2 (`docs/control-panel.md`)** — done: `2026-11-20` (đóng băng node mount, exit codes 0/1/2, TSV schemas, EOL warn policy; `list-sites.sh --json` cho panel)

## Thứ tự thực hiện (bắt buộc)

1. `3.1` trước `2.x` (đóng băng tên mount/port/program) — đã xong.
2. `2.1` trên image tag mới (`customer-runtime:node-shared`), giữ tag `pilot` cho customer cũ.
