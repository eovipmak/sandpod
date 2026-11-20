# Control panel contract

Boundary between this repo (mechanism) and the future control panel
(policy). The image never contains customer domains, site data, or per-site
configuration. The panel owns everything customer-specific.

## Guaranteed by the image (stable interface)

The panel may rely on these without inspecting image internals:

- Mounts: `~/data` → `/srv/customer` (writable), `~/config` →
  `/etc/sandpod-config` (read-only inside the container, so the panel must
  edit the host-side files), host PHP runtimes read-only at `/opt/remi`,
  `/etc/opt/remi`, `/usr/lib64/libonig.so.105`.
- PHP-FPM listeners (fixed ports, one master per minor):
  8.2 → `127.0.0.1:9082`, 8.3 → `127.0.0.1:9000`, 8.4 → `127.0.0.1:9084`,
  8.5 → `127.0.0.1:9085`, 8.6 → `127.0.0.1:9086`.
- CLI binaries: `php82/83/84/85` (`php` = 8.3), `node`/`node-22` (22),
  `node-24`, `node-26` with matching `npm`/`npm-24`/`npm-26`,
  `python3.12`/`python3.13`/`python3.14` (`python3` = 3.12).
- Supervisord services: `sshd`, `crond`, `php82-fpm`, `php83-fpm`,
  `php84-fpm`, `php85-fpm`, `php86-fpm`, `nginx`.
- Nginx variables with defaults: `$site_root` defaults to
  `/srv/customer/public_html`; `$php_fpm_backend` defaults to
  `127.0.0.1:9000` (PHP 8.3). Both maps live in the mounted
  `~/config/nginx.conf`.
- Reload helpers: in-container `sandpod-runtime-reload {8.2|8.3|8.4|8.5|8.6}`
  (config-test + `USR2` to one FPM master); host-side
  `shared-runtimes/scripts/reload-customer-php.sh <service-user>
  <container> <minor>` which calls it via `podman exec`.
- The `php8X.test` hostnames in the shipped `nginx.conf` are pilot test
  fixtures only, not production domains.

## Owned by the panel (not in this repo)

- Site directories: primary domain in `~/data/public_html`, each add-on
  domain in `~/data/<domain>`.
- `~/config/nginx.conf` map entries: which `Host` serves which directory
  (`$site_root`) and which PHP version (`$php_fpm_backend`). Use the
  `customer-apps` module scripts (`add-site.sh`, `set-php-version.sh`,
  `remove-site.sh`, `list-sites.sh`) - never hand-edit with `sed`. Port
  apps (`proxy_pass` per `Host`) are expressed as generated per-domain
  `server` blocks in `~/config/sites/<domain>.conf` via
  `customer-apps/scripts/add-port-app.sh` (nginx include) plus a persistent
  process in `~/config/supervisor/<domain>.conf` (supervisord include).
- Customer port apps: choosing the runtime binary, starting/stopping the
  process, keeping it alive, and reloading Nginx afterwards
  (`nginx -t` then `nginx -s reload` via `podman exec`). Implemented by
  `add-port-app.sh` / `remove-port-app.sh` (`supervisorctl reread &&
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
- `list-sites.sh [--json]`: default text output is stable; `--json` prints
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
