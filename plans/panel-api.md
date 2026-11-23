# Plan: modules/panel-api — expose mechanism cho control panel

> Scope: `docs/scope.md` §2 (cơ chế per-site, registry, reload/report) + §3 (panel sở hữu UI/billing/auth end-user).
> Rule `docs/plan.md` #5: đụng contract panel (mount/port/program/TSV) phải bump Spec trước code → Spec v4 trong `docs/control-panel.md`.
> Repo-only: không ssh/sudo/host thật, không E2E, chỉ `bash -n`, `python3 -m py_compile`, fixture trong `/tmp`.
>
> **Rule bắt buộc (chống đề xuất thừa): mỗi bullet trong §2 phải có 1 dòng
> `scope:` trả lời scope.md §4 trước khi code — `in-scope vì <cơ chế panel nào>`
> hoặc `out-of-scope → <owner>`, fail 1/3 là xóa bullet, không code. Bullet
> nào thiếu dòng `scope:` coi như chưa duyệt.**

## 0. Quyết định đã chốt

* Transport: HTTP daemon chạy **as root trên host**, listen `127.0.0.1:8741`. Panel remote đi qua edge/mTLS của tầng panel, không làm trong repo.
* Stack: **FastAPI + uvicorn** (đã chốt thay stdlib prototype; stdlib `server.py` giữ làm fallback). Venv `/opt/sandpod/panel-api-venv`, requirements trong `modules/panel-api/requirements.txt`.
* Auth: `Authorization: Bearer <token>` đọc từ file `0600` (`/etc/sandpod/panel-api.token`). Token do panel cấp/rotate, repo chỉ đọc.
* Thực thi: `subprocess` gọi đúng scripts hiện tại, không re-implement logic. Giữ nguyên exit codes `0/1/2` → `200/500/400`.
* Sync vs async: tác vụ <5s → sync; provision/remove/backup/rollout → async job (`202 + jobId`, poll `GET /v1/jobs/{id}`).
* Registry vẫn là source-of-truth (`customers.tsv`/`sites.tsv`); panel DB tự sync, không để API ghi DB panel.

## 1. Contract — Spec v4 (đã ghi vào docs/control-panel.md)

* Base: `http://127.0.0.1:8741/v1`, JSON request/response, lỗi `{error, exitCode, stderr}`.
* `GET /v1/customers` ← `customers.tsv`; `GET .../sites` ← `list-sites.sh --json` (panel cấm parse text).
* `POST sites` ← `add-site.sh <user> <container> <domain> [--php] [--primary]`; `PATCH` ← `set-php-version.sh`; port-app ← `add-port-app.sh --port 3000-3999 --command` (panel cấp port, API chỉ validate `valid_port`).
* `POST /v1/customers` (async) ← `provision-customer.sh`; `DELETE` ← `remove-customer.sh` (mặc định giữ data; `--delete-data/--delete-user` phải explicit + confirm).
* DB: `POST /v1/dbs` ← `create-site-db.sh`; trả creds 1 lần, API không lưu disk (ghi rõ trong openapi).
* Rollout: `POST /v1/rollouts/php|app` ← `rollout-*.sh --batch-size --sleep-seconds --skip-update`; dừng batch đầu fail (fix-forward, re-run).
* Xem `modules/panel-api/openapi.yaml` là source-of-truth máy đọc được.

## 2. Việc làm

### P1 — Khung + read-only (panel dùng ngay)
* [x] `modules/panel-api/{openapi.yaml,server.py,README.md,sandpod-panel-api.service,tests/}` (stdlib prototype)
  `scope: in-scope — cơ chế panel đọc registry/report (scope.md §2).`
* [x] FastAPI rewrite (`app.py`+`sh.py`+`jobs.py`, `requirements.txt`): pydantic, `/docs`, `X-Request-ID`, `?dry_run=1` — done `2026-11-23` (`test_api.py` 3 passed; live uvicorn fixture health/docs/dry_run OK)
  `scope: in-scope — cùng cơ chế trên, đổi stack transport; không thêm policy/DNS/TLS.`
* [x] Auth bearer + audit log (journal JSON: rid/method/path/code/latency).
  `scope: in-scope — cơ chế xác thực panel-cùng-host (token file do panel cấp); auth end-user thuộc panel, ngoài scope.`
* [x] Test: `bash -n` mọi script, `python3 -m py_compile`, `pytest` replay fixture TSV + fake script dir (`SANDPOD_SCRIPT_DIR`), assert JSON đúng Spec v2 shape.
  `scope: in-scope — verify repo-only (plan.md rule 2).`

### P2 — Site ops sync (idempotent) — done `2026-11-23`
* [x] `POST/PATCH/DELETE sites`, `POST/DELETE port-apps`, `POST reload {target}` — mỗi script exec đúng 1 lần (test đếm calls).
  `scope: in-scope — cơ chế per-site cho panel (scope.md §2, customer-apps).`
* [x] `stderr EOL warn 8.2` → field `warnings[]` riêng, không chặn; `valid_domain/port/command/minor` lỗi → `400` (pydantic shape lỗi vẫn `422`).
  `scope: in-scope — passthrough validation của script; quyết định policy (có dùng 8.2 không) thuộc panel.`
* [x] DELETE dùng query `?delete_data=true` (+ `?dry_run`), không dùng body (tương thích client); `DELETE /customers` query `delete_data/delete_user/delete_registry_entry`.
  `scope: in-scope — transport tương thích client, không đổi semantics script.`
* [x] `.bak` rollback giữ nguyên trong script; API forward `exit 1 + stderr` + `warnings` (`raise_script_error`).
  `scope: in-scope — giữ mechanism rollback của script, không thêm policy.`
* [x] Test `test_api.py` P2 (4 tests) + live uvicorn fixture: PATCH 8.2 → warnings, DELETE dry_run cmd, domain xấu → 400.
  `scope: in-scope — verify repo-only.`

### P3 — Async jobs (provision/infra) — done `2026-11-23`
* [x] `jobs.py`: thread + `try_acquire/reserve` theo instance (`409` khi trùng), `submit_chain` (gen+reload 1 job), prune 200 jobs, KHÔNG log stdout/stderr ra journal.
  `scope: in-scope — cơ chế chống double-submit + giữ creds khỏi log; retention/off-host thuộc panel/ops, ngoài scope nên chỉ prune in-memory.`
* [x] Fail-fast `400` trước queue: `validate_provision` mirror script (cpus/memory/io-weight/quota/ports), `--delete-user` cần `--delete-data`, rollout `batchSize>=1`.
  `scope: in-scope — mirror validation script, không thêm quota/policy mới.`
* [x] `backup` trả tar path trong stdout; `create-site-db` creds đúng 1 lần trong `get(jobId)`, `GET /jobs` chỉ summaries (không stdout).
  `scope: in-scope — cơ chế trao creds 1 lần; lưu trữ/vault thuộc panel.`
* [x] `host-nginx/reload` chain gen→reload đúng thứ tự; `fleet` lock chống 2 rollout song song; test `test_p3_jobs.py` 5 tests pass.
  `scope: in-scope — reverse-proxy HTTP thuần + rollout batch (scope.md §2); TLS/ACME/DNS ngoài scope, không đụng.`

### P4 — Hardening + docs (không rate-limit IP: out-of-scope theo scope.md §3, edge team sở hữu; loopback 1 client nên vô tác dụng) — done `2026-11-23`
* [x] systemd hardening (`ProtectSystem=strict`, `PrivateTmp=yes`, `NoNewPrivileges`), enforce token file `0600` fail-closed lúc boot — done `2026-11-23` (`ExecStartPre` check `stat -c %a = 600` + `SANDPOD_REQUIRE_AUTH=1` fail-closed trong `app.py`; dev/fixture không set env vẫn fail-open cho test repo-only).
  `scope: in-scope — hardening daemon mechanism trên host (ngoài phạm vi isolation dữ liệu KH).`
* [x] Self-protection đúng phạm vi thay rate-limit: cap job đồng thời toàn cục (`jobs._MAX_CONCURRENT=10` → `409`), body-size limit (`1 MiB` → `413`), timeout (đã có 1800s/7200s) — done `2026-11-23`.
  `scope: in-scope — giới hạn tài nguyên daemon tự bảo vệ; rate-limit per-site/DDoS/WAF → edge team, ngoài scope.`
* [x] `README.md` vận hành: tạo token, start service, curl ví dụ, log xem ở đâu — done `2026-11-23` (`journalctl -u`, `stat -c %a`, caps doc).
  `scope: in-scope — operator docs cho cơ chế.`
* [x] Cập nhật `docs/plan.md` đánh dấu done từng phase — done `2026-11-23` (P4 checkboxes dưới này).
  `scope: in-scope — docs nội bộ repo.`

## 3. Verify (repo-only)

```sh
bash -n modules/panel-api/*.sh 2>/dev/null; python3 -m py_compile modules/panel-api/server.py
python3 -m pytest modules/panel-api/tests/ -q
python3 -c "import yaml,sys; yaml.safe_load(open('modules/panel-api/openapi.yaml'))" # hoặc python json check nếu thiếu pyyaml
grep -c '^  /v1/' modules/panel-api/openapi.yaml  # contract coverage
```

Không làm: start service thật, provision thật, curl host thật (cần host/E2E, để phase sau khi có yêu cầu explicit).

## 4. Scope test (scope.md §4) — chạy cho TỪNG bullet trước khi code

1. Có phải **"cách ly customer với nhau"** hoặc **"cơ chế để panel điều phối"** không? Fail → xóa bullet, ghi `scope: out-of-scope → <owner>`.
2. Làm được ở mức repo + fixture, không cần sở hữu tầng khác? Fail → không vào plan.
3. Có đụng contract panel? Có → Spec bump trước code (rule 5 plan).

Ví dụ bullet lỗi đã bắt được: `rate-limit theo IP` — fail (1): là WAF/edge, owner edge team (scope.md §3) + loopback 1 client nên vô tác dụng → thay bằng cap job/body-size/timeout (mục P4).
