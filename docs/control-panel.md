# Control panel contract

Boundary between this repo (mechanism) and the future control panel
(policy). The image never contains customer domains, site data, or per-site
configuration. The panel owns everything customer-specific.

## Guaranteed by the image (stable interface)

The panel may rely on these without inspecting image internals:

- Mounts: `~/data` → `/srv/customer` (writable), `~/config` →
  `/etc/sandpod-config` (read-only inside the container, so the panel must
  edit the host-side files), host PHP runtimes read-only at `/opt/remi`,
  `/etc/opt/remi`, `/usr/lib64/libonig.so.105`, host Node.js trees
  read-only at `/opt/sandpod/node`, host Python prefixes read-only at
  `/opt/sandpod/python` (see Spec v3).
- PHP-FPM listeners (fixed ports, one master per minor):
  8.2 → `127.0.0.1:9082`, 8.3 → `127.0.0.1:9000`, 8.4 → `127.0.0.1:9084`,
  8.5 → `127.0.0.1:9085`, 8.6 → `127.0.0.1:9086`.
- CLI binaries: `php82/83/84/85` (`php` = 8.3), `node`/`node-22` (22),
  `node-24`, `node-26` with matching `npm`/`npm-24`/`npm-26`,
  `python3.12`/`python3.13`/`python3.14` (`python3` = 3.12).
  When the shared mounts are present the `/usr/local/bin` names resolve
  to `/opt/sandpod/node/<major>/bin/...` and
  `/opt/sandpod/python/<ver>/bin/...`; when absent (old `pilot` tag)
  they fall back to the image RPM binaries at the same names.
- Supervisord services: `sshd`, `crond`, `php82-fpm`, `php83-fpm`,
  `php84-fpm`, `php85-fpm`, `php86-fpm`, `nginx`.
- Nginx variables with defaults: `$site_root` defaults to
  `/srv/customer/public_html`; `$php_fpm_backend` defaults to
  `127.0.0.1:9000` (PHP 8.3). Both maps live in the mounted
  `~/config/nginx.conf`.
- Reload helpers: in-container `sandpod-runtime-reload {8.2|8.3|8.4|8.5|8.6}`
  (config-test + `USR2` to one FPM master); host-side
  `sandpodctl reload-customer-php <service-user>
  <container> <minor>` which calls it via `podman exec`.
- The `php8X.test` hostnames in the shipped `nginx.conf` are pilot test
  fixtures only, not production domains.

## Owned by the panel (not in this repo)

- Site directories: primary domain in `~/data/public_html`, each add-on
  domain in `~/data/<domain>`.
- `~/config/nginx.conf` map entries: which `Host` serves which directory
  (`$site_root`) and which PHP version (`$php_fpm_backend`). Use the
  `customer-apps` module scripts (`sandpodctl add-site`, `sandpodctl set-php-version`,
  `sandpodctl remove-site`, `sandpodctl list-sites`) - never hand-edit with `sed`. Port
  apps (`proxy_pass` per `Host`) are expressed as generated per-domain
  `server` blocks in `~/config/sites/<domain>.conf` via
  `sandpodctl add-port-app` (nginx include) plus a persistent
  process in `~/config/supervisor/<domain>.conf` (supervisord include).
- Customer port apps: choosing the runtime binary, starting/stopping the
  process, keeping it alive, and reloading Nginx afterwards
  (`nginx -t` then `nginx -s reload` via `podman exec`). Implemented by
  `sandpodctl add-port-app` / `sandpodctl remove-port-app` (`supervisorctl reread &&
  supervisorctl update` after the nginx gate, `.bak` rollback on failure).
- App port allocation: each customer app listens on `127.0.0.1` only, one
  port per app from range `3000-3999`. The panel allocates ports from this
  range and records them in its own database (source of truth, never probe the
  container). The generated `sites/*.conf` server block proxies `/` to the
  allocated port with `Host` + `X-Forwarded-*` headers; app ports are
  loopback-only; nothing customer-run is reachable except through the
  container's Nginx on port 80.
- Per-customer provisioning: service account, ports, `~/data` + `~/config`
  (copy the shipped `nginx.conf` as a starting template), SSH keys,
  health checks, and batched PHP patch rollouts.

## Out of scope for the image

Customer domains, app code, per-site Nginx blocks, process management for
customer apps, TLS, database wiring, quotas, and backups. Those belong to
the panel / provisioning / host-nginx / host-mariadb modules.

## Spec v2 (frozen 2026-11-20, Pha 3.1)

Additions to the guaranteed interface above; everything already guaranteed
stays unchanged (no renames of mounts, ports, or program names without a
spec bump):

- Extra mount (when provisioned with the `node-shared` image): host
  `/opt/sandpod/node` → read-only `/opt/sandpod/node` in the container
  (`node/22|24|26/bin/node`, `default -> 22`). Panel port-app commands
  should prefer the absolute shared path and fall back to the image
  `node/node-*/npm*` when the mount is absent (old `pilot` tag).
- Exit codes for all `customer-apps` / `customer-provisioning` scripts:
  `0` ok, `1` runtime failure (a `.bak` rollback was attempted),
  `2` bad arguments/input (bad domain, port, minor, missing flag).
- `sandpodctl list-sites [--json]`: default text output is stable; `--json` prints
  `{"defaults": {"site_root", "php_fpm_backend"}, "php_sites":
  [{"domain", "root", "backend"}], "port_apps": [{"domain", "port",
  "target", "program", "command", "directory"}]}` (missing map entries are
  `null`). The panel must parse `--json`, never grep the text format.
- Registry schemas (TAB-separated, `#` comments):
  `/etc/sandpod/customers.tsv` =
  `instance service_user container web_port ssh_port image`;
  `/etc/sandpod/sites.tsv` = `instance domain` (first domain canonical).
- EOL policy: scripts warn on stderr for near-EOL minors (currently PHP
  8.2, support ends Dec 2026) but still execute — the panel decides whether
  to proceed.

## Spec v3 (frozen 2026-11-23, Pha 2.1)

Shared Node.js / Python runtimes. Everything in Spec v2 stays unchanged.

- Host layout (managed by `shared-runtimes` install/update scripts, root):
  - Node.js: `/opt/sandpod/node/<major>/bin/node` for `<major>` in
    `22, 24, 26`, plus `default -> 22`. Installed with `fnm`
    (`FNM_DIR=/opt/sandpod/node/.fnm`); the `<major>` symlinks point at
    the fnm-managed installation so container paths stay stable across
    patch upgrades. Direct tarballs (`--no-fnm`) keep the same layout.
  - Python: self-contained prefixes `/opt/sandpod/python/<ver>/`
    (`<ver>` in `3.12, 3.13, 3.14`, `default -> 3.12`), each with
    `bin/python3.<minor>`. Managed with `uv`
    (`UV_PYTHON_INSTALL_DIR=/opt/sandpod/python/.uv`); the `<ver>`
    symlinks point at the uv-managed install dir so container paths stay
    stable across patch upgrades. Direct standalone tarballs (`--no-uv`)
    keep the same layout.
- Extra mounts (read-only, host → container, same paths):
  `/opt/sandpod/node` and `/opt/sandpod/python`. Old `pilot` images
  work without them (image RPM fallback); new images use them when present
  and fall back to the image RPMs when absent (same fallback, see E2E in
  `plans/container-image.md`).
- Panel port-app commands should prefer the absolute shared paths
  (`/opt/sandpod/node/<major>/bin/node`,
  `/opt/sandpod/python/<ver>/bin/python3.<minor>`) and fall back to
  the bare `node/node-*/python3.*` names when the mount is absent.
- Reload/rollout helpers: `sandpodctl reload-customer-app <service-user>
  <container> <domain>` restarts one port app (`supervisorctl restart`,
  runtime-agnostic); `sandpodctl rollout-app-patch`
  updates host Node/Python trees then restarts every port app per
  customer in batches with health-checks (mirrors `sandpodctl rollout-php-patch`
  but for port apps: no `USR2`, a restart picks up the new binaries).

## Spec v4 (draft 2026-11-23, Pha panel-api)

Machine-readable contract: `modules/panel-api/openapi.yaml`. Quy tắc bất biến:

- Base `http://127.0.0.1:8741/v1`, JSON. Daemon chạy as root trên host,
  listen loopback-only. Auth `Bearer` (token file `0600` do panel cấp);
  TLS/mTLS cho panel remote thuộc tầng edge/panel, ngoài scope repo.
- Exit → HTTP: script `0` → `2xx`, `2` (bad args) → `400`, `1` (runtime) →
  `500` kèm `{error, exitCode, stderr}`. Warn EOL (PHP 8.2) passthrough
  stderr, không đổi thành lỗi.
- Sync (<5s): `GET customers/sites/resources/health`, mọi site/port-app
  ops. Async (`202 + {jobId}`, poll `GET /v1/jobs/{id}`): provision,
  remove, backup, DB create, ssh-key add, host-nginx reload, rollout php/app.
- Không re-implement logic: mọi `POST/PATCH/DELETE` chỉ gọi đúng script
  trong bảng `plans/panel-api.md` §1 với cùng validation
  (`valid_domain/minor/port`, port-app `3000-3999` do panel cấp).
  `DELETE` dùng query flags (`?delete_data/delete_user/delete_registry_entry/dry_run`),
  không dùng body (tương thích client; chỉ dùng query).
  `list-sites --json` và `resource-report --json` là shape trả về,
  panel không parse text.
- Registry `customers.tsv`/`sites.tsv` vẫn là source-of-truth phía host;
  `sandpodctl create-site-db` trả creds một lần, API không lưu disk.
