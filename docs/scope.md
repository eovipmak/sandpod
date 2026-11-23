# Phạm vi module (scope charter)

> Charter này là thước đo cho mọi đề xuất tương lai. Đọc trước khi thêm task
> vào `plan.md`. Sửa charter phải được duyệt explicit (xem mục 5).

## 1. Repo này là gì

Repo là **1 module isolation** trong hệ control panel lớn hơn (vai trò như
CloudLinux): **cách ly customer với nhau** và **cung cấp cơ chế (mechanism)
cho panel điều phối**. Repo không phải edge, không phải panel, không phải
dịch vụ hướng end-user.

Nguyên tắc mechanism vs policy (mở rộng từ `docs/control-panel.md` ra toàn
repo): repo cung cấp cơ chế — scripts, units, config mẫu, registry schema,
reload/report helpers. Mọi **policy** — domain nào, ai được mấy CPU, cấp cert
cho ai, giá tiền, ai được SSH vào đâu, giữ backup bao lâu — thuộc panel hoặc
đội sở hữu tầng đó.

## 2. IN SCOPE — trách nhiệm của repo

| Trách nhiệm | Module sở hữu |
| --- | --- |
| Image customer: sshd, cron, nginx gateway nội bộ, PHP-FPM đa bản, Node/Python | `customer-container` |
| Runtime share read-only từ host | `shared-runtimes` |
| Service account rootless, Quadlet units, lingering, loopback ports, SSH key injection (cơ chế) | `customer-provisioning` |
| Cách ly CPU/RAM/IO/pids (caps) + XFS project quota | quadlet template + `host-storage` |
| Cơ chế per-site cho panel: nginx maps, PHP version, port apps, supervisor includes | `customer-apps` |
| Registry schemas `customers.tsv` / `sites.tsv` | `customer-provisioning` |
| Rollout patch: reload helpers + chạy batch + health-check | `shared-runtimes` + `monitoring-backup` |
| Báo cáo tài nguyên (chỉ xuất dữ liệu, không kèm policy alert/paging) | `monitoring-backup` |
| Reverse-proxy **HTTP thuần** ra loopback ports + handoff `X-Forwarded-Proto` | `host-nginx` |
| MariaDB share: DB/user per-site + backup/restore scripts (cơ chế) | `host-mariadb` |
| Backup local: tar `data/config` + dump DB (cơ chế; retention/off-host là policy ngoài) | `monitoring-backup` + `host-mariadb` |

## 3. OUT OF SCOPE — kèm owner

| Mục | Owner | Ghi chú |
| --- | --- | --- |
| TLS termination, cấp/gia hạn chứng chỉ, ACME, `listen 443` | edge / control-panel | Cấm chứa certbot/ACME issuance hay hẹn "làm TLS sau" trong repo |
| DNS | panel / registrar | |
| Mail stack | đội mail / panel | |
| Panel UI/API/billing, end-user auth, source of truth phân bổ (DB ports...) | control panel | Repo chỉ định schema TSV, panel giữ policy |
| Nội dung site, app code, domain của khách, chính sách ai được SSH | khách / panel | Repo chỉ cấp cơ chế inject key |
| WAF, rate limit, cache per-site, HTTP/3, chống DDoS | edge team | |
| Retention/off-host backup, alerting/paging policy | ops / panel | Repo chỉ xuất dữ liệu (`resource-report --json`) |
| Hardening OS host ngoài phạm vi isolation | host admin | |

## 4. Scope test — mọi đề xuất tương lai phải trả lời trước khi vào plan

1. Có phải **"cách ly customer với nhau"** hoặc **"cơ chế để panel điều phối"** không? Nếu là dịch vụ hướng end-user/Internet → ngoài phạm vi.
2. Có làm được ở mức repo + fixture, **không cần sở hữu tầng khác** (edge/DNS/panel DB) không?
3. Có đụng contract panel (mount/port/program/TSV) không? Có → spec bump trước (rule 5 plan).

Fail 1/3 = ngoài phạm vi → không vào plan.

## 5. Muốn mở rộng scope thì sao

Chỉ khi được duyệt explicit, và phải cập nhật `scope.md` này **trước** khi code
(cùng cách làm như spec bump rule 5), ghi rõ owner nhận trách nhiệm mới.
