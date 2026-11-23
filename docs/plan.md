# SandPod — Kế hoạch Pha 2 + 3

> Baseline: `2026-11-20`. Pha 0/1 vẫn là chặn production, Pha 2+3 dưới đây làm sau trên nhánh/image mới, không đụng pilot `001`.

## Quy tắc (bắt buộc)

1. Không đụng host: không ssh/sudo/thi hành trên host, không provision/rebuild/restart thật.
2. Không verify E2E: chỉ verify mức repo (`bash -n`, đối chiếu `grep`, test logic trích rời với fixture trong `/tmp`).
3. Mọi task trong plan phải xong được ở mức repo; việc cần host/E2E không cho vào plan khi chưa được yêu cầu explicitly.
4. Không xóa bản PHP nào — giữ đủ bản cho KH lựa (quyết định `2026-11-20`).
5. Đụng contract panel (mount/port/program/TSV) phải bump Spec trong `docs/control-panel.md` trước khi code.
6. Scope: repo là 1 module isolation (vai trò như CloudLinux), xem `docs/scope.md` trước khi đề xuất. TLS/DNS/mail/WAF/panel-UI thuộc tầng khác, ngoài scope; `host-nginx` chỉ reverse-proxy HTTP thuần.

## Pha 2: Runtime & Scale

- [x] **2.1 Shared Node/Python** — host quản lý, container mount RO, image gọn dần — done: `2026-11-23` (repo-only, image tag mới)
  - [x] `install-node-runtimes.sh` (fnm + stable `<major>` symlink, `--no-fnm` tarball fallback) + `update-node-runtimes.sh` + `install-python-runtimes.sh` (uv + stable `<ver>` symlink, `--no-uv` standalone-tarball fallback) + `update-python-runtimes.sh` (SELinux label, verify `--version`) — done: `2026-11-23`
  - [x] `Containerfile` placeholder `/opt/sandpod/node` + `/opt/sandpod/python`, symlink `/usr/local/bin/node*/npm*/python3*` → shared trees (dangling khi thiếu mount → fallback RPM `/usr/bin` qua PATH) — done: `2026-11-23`
  - [x] `quadlet/customer@.container` mount RO `/opt/sandpod/node` + `/opt/sandpod/python` — done: `2026-11-23`
  - [x] `reload-customer-app.sh` (`supervisorctl restart app-<domain>`, runtime-agnostic) + `rollout-app-patch.sh` (update cả 2 trees → restart mọi port app theo `list-sites --json`, batch + health-check) + docs — done: `2026-11-23`
  - [x] Spec v3 (`docs/control-panel.md`): fnm layout, python prefix, mount mới, panel dùng absolute shared path + fallback, helper reload/rollout — done: `2026-11-23`
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
- [x] **3.2 Spec v3 (`docs/control-panel.md`)** — done: `2026-11-23` (fnm `/opt/sandpod/node/<major>`, uv `/opt/sandpod/python/<ver>`, mount RO mới, panel dùng absolute path + fallback, `reload-customer-app` / `rollout-app-patch`)
- [x] **3.3 panel-api P1-P4 (Spec v4, `modules/panel-api/`, xem `plans/panel-api.md`)** — done: `2026-11-23` (repo-only, không host/E2E)
  - [x] P1 khung + read-only + FastAPI rewrite (`app.py`+`sh.py`+`jobs.py`, `/docs`, `X-Request-ID`, `?dry_run=1`) + auth bearer + audit log
  - [x] P2 site ops sync (`POST/PATCH/DELETE sites`, `POST/DELETE port-apps`, `POST reload`, EOL `warnings[]`, DELETE query flags, `.bak` rollback passthrough)
  - [x] P3 async jobs (provision/remove/backup/DB/nginx-reload/rollout `202+jobId`, lock instance `409`, fail-fast `400`, creds 1 lần, fleet lock)
  - [x] P4 hardening + docs (systemd `ProtectSystem=strict`, token `0600` fail-closed, cap 10 jobs `409` + body 1 MiB `413` + timeout, `README.md` vận hành; không rate-limit IP — edge-owned, loopback vô tác dụng)

## Thứ tự thực hiện (bắt buộc)

1. `3.1` trước `2.x` (đóng băng tên mount/port/program) — đã xong.
2. `2.1` trên image tag mới (`customer-runtime:node-shared`), giữ tag `pilot` cho customer cũ.
