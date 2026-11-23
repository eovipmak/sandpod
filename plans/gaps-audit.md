# SandPod — Gaps audit (ngoài E2E) + Plans

> Nguồn: rà soát repo-only `2026-11-23`. E2E để sau cùng (rule `docs/plan.md` #2).
> Quy ước: `- [ ]` chưa làm, `- [x]` done. Mỗi Part làm ở mức repo + fixture `/tmp`, verify `bash -n`, `py_compile`, `pytest`.

## Part 1 — Contract panel-api drift (nặng nhất) — done `2026-11-23` (18 passed)
- [x] `DELETE sites`: `openapi.yaml` body `{deleteData}` vs `app.py` + `README` query `?delete_data=true` vs `server.py` body → chốt query, sửa spec trước code
- [x] `DELETE /customers/{id}`: `openapi.yaml` thiếu query `delete_data/delete_user/delete_registry_entry/dry_run` (`app.py:401-403` có)
- [x] `DELETE port-apps/{domain}`: thiếu query `?delete_data/dry_run` (`app.py:513-514` có)
- [x] `server.py` fallback thiếu prefix `/v1` (`/dbs`, `/rollouts/php|app`, `/host-nginx/reload`) + `DELETE` đọc body vs query
- [x] Spec thiếu: `GET /openapi-sync-check` public không auth, `/docs /openapi.json`, `?dry_run + X-Request-ID`, `409/413` cho hầu hết mutating endpoints
- [x] `control-panel.md:142-144` async-list thiếu `ssh-keys`; `plans/panel-api.md §1` thiếu `ssh-keys/reload/resources/jobs`

## Part 2 — Auth / validation / timeout — done `2026-11-23` (24 passed)
- [x] Fail-open mặc định; `token 0600` per-request khi `SANDPOD_REQUIRE_AUTH=1` (đổi mode sau boot → 500); `sync-check` đã auth từ P1; ssh-key temp tự unlink sau job (`jobs.submit cleanup=[...]`), submit-throw cũng dọn
- [x] Pydantic `422` giữ cho lỗi shape (đã document trong README P2); custom validators → 400; `validate_provision` thêm `serviceUser` pattern, `image` non-empty, `quotaGb 1-1000`; `DOMAIN_RE` đã parity `lib.sh` (không đổi); `check_command` giữ shell-meta (chạy as customer trong container, panel sở hữu) + bound 8192; `SshKey` whitelist type+base64; `Db` max 64 (yaml cũng `maxLength`)
- [x] `_rollout` int-safety (null/"abc" → 400/422, không 500); `lookup/get_job/404` shape `{error,exitCode,stderr}`; `resources` miss → 404 thay `{}` 200; atomic `jobs.acquire()` thay cặp `ensure_capacity→try_acquire` (6 call sites); `submit/submit_chain` dọn reservation khi Thread-throw
- [x] Body-cap: giữ header check + thêm chunked-read (Starlette cache body); `server.py:_body` cap 1 MiB + marker `_too_large/_bad_json` → 413/400; rollout fallback `timeout=7200`

## Part 3 — Script guard / thiếu validation — done `2026-11-23` (bash-n OK, 24 passed, logic trích rời /tmp OK)
- [x] `install-node/python`: `rm -rf $dest` chỉ sau khi fresh tree verify chạy được; `--prefix` từ chối system path + version `X.Y.Z`; updater cũng guard `$PREFIX` trước `restorecon -RF`
- [x] `provision-customer`: `IMAGE` regex + `quota/inodes` validate sớm (exit 2 trước khi tạo user); `IMAGE_TAR 0600` + trap dọn; `--skip-start --ssh-key` WARNING stderr (-> panel `warnings[]`); bump port dưới lock -> daemon-reload + restart khi đổi
- [x] `remove-customer` + `set-customer-quota`: từ chối `HOME` rỗng/`/`/ngoài `/home` trước mọi `rm -rf`/`xfs_quota -p`
- [x] Backup/restore: `backup-customer` validate `INSTANCE`/`KEEP` + prune `nullglob` sort theo tên (không `ls`); `backup-site-db` tmp+`gzip -t`+size check+`--dir` tuyệt đối; `restore-site-db` `gzip -t`+backup trước+`CREATE DATABASE IF NOT EXISTS`; rollout `batch-size/sleep` số + `--minor 8.2-8.6`
- [x] `generate-site-conf`: validate domain/port, trùng domain giữ first + warn, `server_name` sanitize, stale warn + `--prune`, bỏ biến chết `TEMPLATE`
- [x] `resource-report` `json_escape` thêm control chars; `health-check` `--max-time 10` + port 1-65535 + check `curl`
- [x] `Containerfile`: RPM `node-22`/`python3.12` explicit thực chất KHÔNG thiếu (`nodejs`=22.x, `python3`=3.12 trên AlmaLinux 10 — đã chú thích + `npm-22` + `check-runtime-mounts`, repo-only `2026-11-23`); placeholder `libonig`, `__PASSWORD__` + full E2E đã verify trên host `2026-11-23` (image `node-shared`, xem `plans/container-image.md`) — done, container E2E đã dọn

## Part 4 — Unit/timer + docs/tests — done `2026-11-23` (bash-n OK, 32 passed)
- [x] Units: `monitoring-backup/systemd/` (backup@ daily + report hourly), `host-mariadb/systemd/` (db-backup@ daily), `host-nginx/systemd/` (regen path khi TSV đổi); `sandpod.cnf` template; logrotate 14 ngày; operator `cp + enable` trong README (timers chỉ chạy cơ chế, retention/off-host/paging vẫn policy ngoài)
- [x] Firewall/SELinux (repo-only): firewalld rich-rule ví dụ + pasta-gateway doc + REQUIRE SSL ghi rõ ngoài scope; `restorecon` guarded cho `conf.d/sandpod-*.conf`, backup dirs, `/etc/projid|projects`; user `'%'` + bind doc tightening khi pin được subnet (cần host verify)
- [x] Docs: `architecture.md` panel-api (8741/root-loopback/token0600/sync-async), `scope.md` exception daemon-hardening, `plans/panel-api.md` counts (suite 32) + P4 fail-closed per-request/atomic-acquire, READMEs install notes + verify mới
- [x] Tests `test_p4_ops.py` 8: 401/500 fail-closed + public health/docs, cap 409, body 413, prune bound, tmp-cleanup + summaries không stdout, sync fail shapes (400 exitCode 2 / 500), `server.py` parity strings (`/v1/*`, query flags, 413, timeout 7200)

