# Shared runtimes (host-managed, mounted read-only into customers)

PHP, Node.js, and Python live on the host. Customer containers mount the
trees read-only; each customer runs its own processes inside its rootless
container and is gracefully reloaded/restarted per customer after an update.

## Runtime layout

- PHP 8.2: `/opt/remi/php82/root/usr`, config `/etc/opt/remi/php82`
- PHP 8.3: `/opt/remi/php83/root/usr`, config `/etc/opt/remi/php83`
- PHP 8.4: `/opt/remi/php84/root/usr`, config `/etc/opt/remi/php84`
- PHP 8.5: `/opt/remi/php85/root/usr`, config `/etc/opt/remi/php85`
- PHP 8.6: `/opt/remi/php86/root/usr`, config `/etc/opt/remi/php86`
- Node.js 22/24/26: `/opt/sandpod/node/22|24|26` (stable symlinks),
  `default -> 22`. Managed with `fnm` (`FNM_DIR=/opt/sandpod/node/.fnm`);
  `--no-fnm` installs official linux-x64 tarballs into the same layout.
- Python 3.12/3.13/3.14: self-contained `/opt/sandpod/python/3.12|3.13|3.14`
  (stable symlinks, `bin/python3.<minor>`), `default -> 3.12`. Managed with
  `uv` (`UV_PYTHON_INSTALL_DIR=/opt/sandpod/python/.uv`);
  `--no-uv` downloads `python-build-standalone` `install_only` tarballs into
  the same layout.

The RPM/package manager keeps the newest patch installed for each minor
namespace (PHP). The customer-facing selection is 8.2–8.6 (PHP), 22/24/26
(Node), 3.12/3.13/3.14 (Python); patch display can be added to the control
panel later. PHP 8.0/8.1 are end-of-life and intentionally omitted.

## Install

Run `sandpodctl install-{php,node,python}-runtimes` as root. PHP installs the parallel
FPM/CLI packages plus common extensions, but does not enable or start
PHP-FPM services on the host. Node installs `fnm` to `/usr/local/bin/fnm`
(unless `--no-fnm`) then the three majors. Python installs `uv` to
`/usr/local/bin/uv` (unless `--no-uv`) then the three minors
(pins: `--py312-ver/--py313-ver/--py314-ver`, `--release-tag` only matters
for `--no-uv`). `uv` here only provisions the host trees; customer apps use
the absolute python paths (or `python -m pip`), never a host `uv`.

When SELinux is enforcing, the installers label the shared runtime/config paths
for container reads. Customer containers mount the paths read-only.

## Apply a patch

Update the host trees to the newest patch:

```sh
sudo sandpodctl update-php-runtimes
sudo sandpodctl update-node-runtimes   # [--node22-ver X ...] [--no-fnm]
sudo sandpodctl update-python-runtimes # [--py312-ver X ...]
```

Package files update once on the host. Existing processes continue
using code already loaded in memory, so reload/restart inside each customer
container:

```sh
# PHP: graceful reload of one FPM minor (config-test + USR2, no restarts):
sudo sandpodctl reload-customer-php \
  svc-c001 customer-001 8.3

# Node/Python port app: restart one supervisord program (picks up new
# /opt/sandpod binaries, runtime-agnostic):
sudo sandpodctl reload-customer-app \
  svc-c001 customer-001 node.example.com
```

The PHP helper gracefully reloads that PHP version without recreating the
container or restarting Nginx, SSH, cron, or other PHP-FPM minors. The app
helper restarts one port app. Roll out to customers in batches and health-check
each one — fleet wrappers are subcommands too:
`sandpodctl rollout-php-patch` (per-minor `USR2`) and `sandpodctl rollout-app-patch`
(restarts every port app per customer, `--skip-node-update` /
`--skip-python-update` available).

The provisioning/update controller should call the reload helpers once per
customer using the customer-to-service-account/container mapping.

## Scope

PHP 8.2–8.6 use shared read-only Remi host runtimes. Node.js 22/24/26 uses
shared read-only host trees at `/opt/sandpod/node/<major>` (fnm-managed,
stable symlinks hide fnm's versioned dirs from the panel). Python
3.12/3.13/3.14 uses self-contained shared prefixes at
`/opt/sandpod/python/<ver>`. Container images keep distro RPM fallbacks at
the same `/usr/local/bin` names so old `pilot` containers without the new
mounts still run.
