# AGENTS.md — SandPod

Small infra repo (no app code, tests, CI, lint, or package manager). Source of truth is executable files (`Containerfile`, `quadlet/*`, `scripts/*`), then `docs/architecture.md`. READMEs duplicate them; trust the executable on conflict.

## Layout

- `docs/architecture.md` — service/module boundaries and module status (see status table there).
- `modules/customer-container/` — AlmaLinux 10 image + supervisord (sshd, crond, 5× php-fpm 8.2-8.6, nginx) + rootless Quadlet template + pilot site. Build/run docs in its `README.md`.
- `modules/shared-runtimes/` — host Remi PHP RPMs + fnm-managed Node 22/24/26 (`/opt/sandpod/node/<major>`, stable symlink qua fnm) + standalone Python 3.12/3.13/3.14 (`/opt/sandpod/python/<ver>`), tất cả mount read-only vào customers. Image giữ RPM fallback khi thiếu mount.
- New feature = new dir under `modules/` owning its build files, units, config, verification, and operator docs; cross-module decisions go in `docs/`.
- Scope: `docs/scope.md` — this repo is one isolation module (CloudLinux role: isolate customers + mechanism for the panel). TLS/DNS/mail/WAF/panel-UI belong to other layers; check the scope test there before proposing features.

## Build / deploy (exact commands)

Build image **as the rootless service account**, from `modules/customer-container/`:

```sh
podman build -t localhost/sandpod/customer-runtime:node-shared .
```

Pilot `001` stays pinned to `:pilot`; new customers default to `:node-shared`.

Per customer: dedicated unprivileged host account with own Podman storage + lingering user manager. Automated by `sandpodctl provision-customer` (manual equivalent: install template + instance symlink (`customer@001.container`) + `.container.d/` drop-in under `~/.config/containers/systemd/`, create `~/data` + `~/config`, copy `config/nginx.conf` → `~/config/nginx.conf`, then:

```sh
systemctl --user daemon-reload
systemctl --user start 'customer@001.service'
```

Pilot ports are loopback-only (`127.0.0.1:18001`→80, `127.0.0.1:22201`→22). New customers need unused ports. Quadlet uses `%h` (account home for `data`/`config`) and `%i` (instance id); limits are 1 CPU (`CPUQuota=100%`/`--cpus=1.0`) / 1 GiB RAM swap 0 (`Memory=1g`, `MemorySwapMax=0`) / 512 pids (`PidsLimit`, `TasksMax=1024`) / fair-share `CPUWeight=100`+`IOWeight=100`, pinned per instance in `20-resources.conf` (`--cpus/--memory/--io-weight` flags).

## Shared PHP runtimes (order matters)

Host scripts run **as root**:

```sh
sudo sandpodctl install-php-runtimes        # first-time host setup, never starts host FPM
sudo sandpodctl update-php-runtimes        # dnf upgrade php82-*/php83-*/php84-*/php85-*/php86-*/oniguruma5php
sudo sandpodctl reload-customer-php svc-c001 customer-001 8.3
```

Patch rollout is `update` → per-customer per-minor `reload-customer-php` (validates FPM config, sends `USR2` to that FPM master) → health-check each customer, in batches. Running FPM keeps old code in memory, so the reload is required; it does not restart container/nginx/SSH/other minors. `reload-customer-php` needs the account's user manager running (`/run/user/<uid>` must exist). In-container equivalent: `sandpod-runtime-reload {8.2|8.3|8.4|8.5|8.6}` (config-test + `USR2`). Fleet wrapper: `sandpodctl rollout-php-patch` (batches + health-checks).

Node/Python patch rollout mirrors PHP: `sandpodctl update-node-runtimes` + `sandpodctl update-python-runtimes` (fnm/uv reinstall + relink) → per-customer per-domain `sandpodctl reload-customer-app` (`supervisorctl restart app-<domain>`, picks up new `/opt/sandpod` binaries) → health-check each customer, in batches. Fleet wrapper: `sandpodctl rollout-app-patch` (`--skip-node-update/--skip-python-update/--skip-update`). Panel port-app commands should prefer absolute shared paths (`/opt/sandpod/node/<major>/bin/node`, `/opt/sandpod/python/<ver>/bin/python3.<minor>`), fallback to bare `node*/python3.*` (image RPMs) when mounts are absent.

## Verification

```sh
curl -H 'Host: php82.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php83.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php84.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php85.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php86.test' http://127.0.0.1:18001/index.php
```

Change a site's PHP version by editing the `map $host $php_fpm_backend` block in the **mounted** `~/config/nginx.conf` (default→`127.0.0.1:9000`/8.3, `php82.test`→9082, `php84.test`→9084, `php85.test`→9085, `php86.test`→9086), then `nginx -t` and reload nginx inside the container. Prefer `sandpodctl set-php-version` over hand-editing.

## Gotchas

- Never mount writable host runtime dirs into customers. Read-only mounts: `/opt/remi`, `/etc/opt/remi`, `/usr/lib64/libonig.so.105` (Containerfile creates an empty placeholder for the last; host file overlays it). Image symlinks `php82/83/84/85` → Remi paths, default `php` → 8.3.
- SELinux enforcing: installer labels `container_file_t` + `restorecon`; updater re-runs `restorecon`. If container can't read runtimes, check labels before changing mounts.
- SSH is `AllowUsers customer`, password/kbd-interactive off, forwarding/agent/X11/tunnel off. Provision `/home/customer/.ssh/authorized_keys` before granting access.
- Supervisor password is randomized at image build (`__SUPERVISOR_PASSWORD__`); don't hardcode it.
- Not production-ready: disk quotas are enforced via XFS projects
  (`host-storage`, soft 8G/hard 10G per account), but no host Nginx/MariaDB
  wiring, no fleet rollback or log rotation. Fleet rollout script exists
  (`sandpodctl rollout-php-patch`). Websites in one customer share a container/filesystem (pools + DB creds are not isolation). PHP 8.2 is near end of security support.
