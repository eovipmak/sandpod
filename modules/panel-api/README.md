# panel-api — FastAPI wrap cho control panel (mechanism only, cùng host)

Daemon chạy **as root**, listen `127.0.0.1:8741`. Mọi endpoint chỉ gọi
backend operator (`sh.py` là nơi duy nhất biết đường dẫn binary),
không re-implement policy. `server.py` stdlib giữ lại làm fallback
không-dependency; production dùng `app.py` (FastAPI).

## P2 — site ops sync (idempotent)

* `POST/PATCH/DELETE /customers/{id}/sites[/{domain}]`, `POST/DELETE port-apps`,
  `POST reload {php-8.x|app-<domain>}` — mỗi call exec script đúng 1 lần.
* Validation `domain/port/command/minor` lỗi → `400 {error, exitCode:2}`
  (lỗi shape JSON pydantic vẫn `422`). Script là source-of-truth cuối.
* EOL warn (PHP 8.2) không chặn: `stderr` giữ nguyên + bóc thành `warnings[]`
  để panel hiển thị đẹp thay vì parse stderr.
* DELETE dùng query `?delete_data=true&dry_run=true`, không dùng body:
  `DELETE /v1/customers/001/sites/shop.example.com?delete_data=true`.

## P3 — async jobs (provision/infra)

* Queue `202 + jobId`, poll `GET /v1/jobs/{id}`; `GET /v1/jobs` chỉ summaries
  (không stdout — creds DB chỉ lấy qua `get(jobId)` đúng 1 lần, panel cất vault).
* Lock theo instance: provision/remove/backup/ssh-key trùng → `409`;
  2 rollout song song → `409`; reload nginx cũng `409` khi đang chạy.
* Fail-fast `400` trước khi queue (sai cpus/memory/port... không tạo job rác).
* `POST /host-nginx/reload` chain gen→reload trong 1 job, dừng khi gen lỗi.
* Journal KHÔNG bao giờ chứa stdout/stderr (creds an toàn); job history
  in-memory, prune 200, restart daemon là mất.

## Stack: FastAPI + uvicorn (đã chốt)

* Validation pydantic → lỗi input trả `422` (panel debug nhanh hơn parse stderr).
* `/docs` Swagger UI ngay trên host cho panel dev bấm thử.
* Jobs async trong `jobs.py` (provision/remove/backup/db/rollout); poll `GET /v1/jobs/{id}`.
* `X-Request-ID`: client gửi hoặc server tự sinh, echo trong header +
  JSON lỗi, log kèm method/path/exit vào journal.

## Chạy (host, operator)

```sh
# Daemon đọc scripts từ checkout tại /opt/sandpod/repo (khớp
# WorkingDirectory trong unit file) — thiếu là fail 200/CHDIR lúc start.
git clone <repo-url> /opt/sandpod/repo  # hoặc symlink tới checkout có sẵn
make -C /opt/sandpod/repo/modules/sandpodctl build  # backend binary daemon gọi
python3 -m venv /opt/sandpod/panel-api-venv
/opt/sandpod/panel-api-venv/bin/pip install -r /opt/sandpod/repo/modules/panel-api/requirements.txt
openssl rand -hex 32 > /etc/sandpod/panel-api.token && chmod 0600 /etc/sandpod/panel-api.token
stat -c %a /etc/sandpod/panel-api.token  # phải in ra 600, service fail-closed nếu khác
cp /opt/sandpod/repo/modules/panel-api/sandpod-panel-api.service /etc/systemd/system/
systemctl daemon-reload && systemctl start sandpod-panel-api.service
```

Service chạy as root, listen loopback-only `127.0.0.1:8741`, hardening
`ProtectSystem=strict + PrivateTmp=yes + NoNewPrivileges=yes`, `ExecStartPre`
từ chối boot khi token thiếu/không `0600` (`SANDPOD_REQUIRE_AUTH=1`).

## Self-protection (đúng phạm vi, thay rate-limit IP)

Per-site rate-limit/DDoS/WAF thuộc edge team (ngoài scope `docs/scope.md` §3,
loopback 1 client nên vô tác dụng) — daemon chỉ tự giới hạn tài nguyên nó giữ:

* Job đồng thời toàn cục: tối đa `10` queued/running (`jobs._MAX_CONCURRENT`),
  vượt → `409`. Lock theo instance vẫn giữ (`409` khi trùng).
* Body-size limit: `1 MiB` (`SANDPOD_MAX_BODY_BYTES`), vượt → `413`.
* Timeout: sync script `120s`, job thường `1800s`, rollout `7200s`.

## Logs / debug

```sh
journalctl -u sandpod-panel-api.service -f  # audit JSON: rid/method/path/code/latency
systemctl status sandpod-panel-api.service
curl -H "Authorization: Bearer $(cat /etc/sandpod/panel-api.token)" http://127.0.0.1:8741/v1/health
```

Journal KHÔNG bao giờ chứa stdout/stderr job (creds DB chỉ lấy 1 lần qua
`GET /v1/jobs/{id}`, panel cất vault ngay). Job history in-memory, prune 200,
restart daemon là mất.

## Debug không cần root-data (?dry_run=1)

Mọi endpoint mutating hỗ trợ `?dry_run=true`: validate đầy đủ rồi trả
`cmd[]` sẽ chạy, không exec. Dùng khi panel dev fixture:

```sh
B=http://127.0.0.1:8741/v1
curl $B/health
curl $B/customers
curl "$B/customers/001/sites?dry_run=true" -H 'Content-Type: application/json' \
  -d '{"domain":"demo.test","php":"8.3"}'
# -> {"dryRun":true,"cmd":[".../sandpodctl","add-site","svc-c001","customer-001","demo.test","--php","8.3"]}
curl -X POST $B/customers/001/backups          # -> 202 {jobId}
curl $B/jobs/<jobId>                           # poll
```

Token: `curl -H "Authorization: Bearer $(cat /etc/sandpod/panel-api.token)" ...`
(`/v1/health` và `/docs` public để readiness/debug.)

## Verify repo-only

```sh
/tmp/sapi-venv/bin/python -m pytest modules/panel-api/tests/ -q  # 33 passed: api/contract/p2-hardening/p4-ops/p3-jobs
for sh in $(find modules -name "*.sh"); do bash -n "$sh"; done
make -C modules/sandpodctl build  # backend binary cho daemon (SANDPOD_SANDPODCTL)
/tmp/sapi-venv/bin/python -m py_compile modules/panel-api/*.py
```

Backend: daemon gọi `sandpodctl <subcommand>` (cùng argv như `.sh` cũ).
Rollback khẩn: `SANDPOD_BACKEND=scripts` để gọi lại `modules/*/scripts/*.sh`.
