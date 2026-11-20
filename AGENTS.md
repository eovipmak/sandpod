# AGENTS.md — SandPod

Small infra repo (no app code, tests, CI, lint, or package manager). Source of truth is executable files (`Containerfile`, `quadlet/*`, `scripts/*`), then `docs/architecture.md`. READMEs duplicate them; trust the executable on conflict.

## Layout

- `docs/architecture.md` — service/module boundaries and module status (see status table there).
- `modules/customer-container/` — AlmaLinux 10 image + supervisord (sshd, crond, 4× php-fpm 8.2-8.5, nginx) + rootless Quadlet template + pilot site. Build/run docs in its `README.md`.
- `modules/shared-runtimes/` — host Remi PHP RPMs mounted read-only into customers. Only PHP 8.2/8.3/8.4/8.5 is shared; Node 22/24/26 and Python 3.12/3.13/3.14 still ship in the image.
- New feature = new dir under `modules/` owning its build files, units, config, verification, and operator docs; cross-module decisions go in `docs/`.

## Build / deploy (exact commands)

Build image **as the rootless service account**, from `modules/customer-container/`:

```sh
podman build -t localhost/sandpod/customer-runtime:pilot .
```

Per customer: dedicated unprivileged host account with own Podman storage + lingering user manager. Automated by `customer-provisioning/scripts/provision-customer.sh` (manual equivalent: install template + instance symlink (`customer@001.container`) + `.container.d/` drop-in under `~/.config/containers/systemd/`, create `~/data` + `~/config`, copy `config/nginx.conf` → `~/config/nginx.conf`, then:

```sh
systemctl --user daemon-reload
systemctl --user start 'customer@001.service'
```

Pilot ports are loopback-only (`127.0.0.1:18001`→80, `127.0.0.1:22201`→22). New customers need unused ports. Quadlet uses `%h` (account home for `data`/`config`) and `%i` (instance id); limits are 1 CPU / 1 GiB / 512 pids.

## Shared PHP runtimes (order matters)

Host scripts run **as root**:

```sh
sudo modules/shared-runtimes/scripts/install-php-runtimes.sh        # first-time host setup, never starts host FPM
sudo modules/shared-runtimes/scripts/update-php-runtimes.sh        # dnf upgrade php82-*/php83-*/php84-*/php85-*/oniguruma5php
sudo modules/shared-runtimes/scripts/reload-customer-php.sh svc-c001 customer-001 8.3
```

Patch rollout is `update` → per-customer per-minor `reload-customer-php` (validates FPM config, sends `USR2` to that FPM master) → health-check each customer, in batches. Running FPM keeps old code in memory, so the reload is required; it does not restart container/nginx/SSH/other minors. `reload-customer-php` needs the account's user manager running (`/run/user/<uid>` must exist). In-container equivalent: `sandpod-runtime-reload {8.2|8.3|8.4|8.5}` (config-test + `USR2`). Fleet wrapper: `monitoring-backup/scripts/rollout-php-patch.sh` (batches + health-checks).

## Verification

```sh
curl -H 'Host: php82.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php83.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php84.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php85.test' http://127.0.0.1:18001/index.php
```

Change a site's PHP version by editing the `map $host $php_fpm_backend` block in the **mounted** `~/config/nginx.conf` (default→`127.0.0.1:9000`/8.3, `php82.test`→9082, `php84.test`→9084, `php85.test`→9085), then `nginx -t` and reload nginx inside the container. Prefer `customer-apps/scripts/set-php-version.sh` over hand-editing.

## Gotchas

- Never mount writable host runtime dirs into customers. Read-only mounts: `/opt/remi`, `/etc/opt/remi`, `/usr/lib64/libonig.so.105` (Containerfile creates an empty placeholder for the last; host file overlays it). Image symlinks `php82/83/84/85` → Remi paths, default `php` → 8.3.
- SELinux enforcing: installer labels `container_file_t` + `restorecon`; updater re-runs `restorecon`. If container can't read runtimes, check labels before changing mounts.
- SSH is `AllowUsers customer`, password/kbd-interactive off, forwarding/agent/X11/tunnel off. Provision `/home/customer/.ssh/authorized_keys` before granting access.
- Supervisor password is randomized at image build (`__SUPERVISOR_PASSWORD__`); don't hardcode it.
- Not production-ready: disk quotas are enforced via XFS projects
  (`host-storage`, soft 8G/hard 10G per account), but no host Nginx/MariaDB
  wiring, no fleet rollback or log rotation. Fleet rollout script exists
  (`monitoring-backup/scripts/rollout-php-patch.sh`). Websites in one customer share a container/filesystem (pools + DB creds are not isolation). PHP 8.2 is near end of security support.
