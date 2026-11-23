# Customer container

This module builds the first rootless customer container prototype.

## Contents

- `Containerfile`: AlmaLinux 10 image and customer-side packages.
- `config/`: SSH, internal Nginx, and process-supervisor configuration.
- `scripts/runtime-reload`: gracefully reload one PHP-FPM minor version.
- `quadlet/customer@.container`: reusable rootless Podman template.
- `quadlet/customer@001.container`: pilot instance symlink.
- `quadlet/customer@001.container.d/`: pilot-specific host ports.
- `examples/pilot-site/`: test HTML and PHP endpoints.

## Runtime matrix

| Runtime | Pilot version | Command / FPM listener |
| --- | --- | --- |
| PHP | 8.2 | `php82`, FPM `127.0.0.1:9082`, host runtime mount |
| PHP | 8.3 | `php83` / default `php`, FPM `127.0.0.1:9000`, host runtime mount |
| PHP | 8.4 | `php84`, FPM `127.0.0.1:9084`, host runtime mount |
| PHP | 8.5 | `php85`, FPM `127.0.0.1:9085`, host runtime mount |
| PHP | 8.6 | `php86`, FPM `127.0.0.1:9086`, host runtime mount |
| Node.js | 22 | `node` / `node-22`, `npm` | shared `/opt/sandpod/node/22` (fnm) |
| Node.js | 24 | `node-24`, `npm-24` | shared `/opt/sandpod/node/24` (fnm) |
| Node.js | 26 | `node-26`, `npm-26` | shared `/opt/sandpod/node/26` (fnm) |
| Python | 3.12 | `python3` / `python3.12` | shared `/opt/sandpod/python/3.12` |
| Python | 3.13 | `python3.13` | shared `/opt/sandpod/python/3.13` |
| Python | 3.14 | `python3.14` | shared `/opt/sandpod/python/3.14` |

PHP executables and extensions are not in the image. They come from the host's
read-only Remi SCL mounts; Node.js and Python come from the host's read-only
`/opt/sandpod/node` / `/opt/sandpod/python` trees (see the
[shared-runtimes module](../shared-runtimes/README.md), Spec v3 in
`docs/control-panel.md`). The image keeps distro RPM fallbacks at the same
`/usr/local/bin` names so containers started without the new mounts still
run. EPEL remains enabled inside the image for tools such as `htop`.

## Build and run

Build the image **as the rootless service account**, from this directory:

```sh
podman build -t localhost/sandpod/customer-runtime:pilot .
```

Before starting the image, build a current shared PHP runtime for each minor
version listed in the runtime matrix; see the [shared-runtime guide](../shared-runtimes/README.md).

For each customer, create a **separate rootless service account** with its own
Podman storage and user systemd manager. Install the template, the instance
symlink, and that instance's `.container.d` drop-in under
`~/.config/containers/systemd/`. Create `~/data` and `~/config`, copy
`config/nginx.conf` into `~/config/nginx.conf`, then run:

```sh
systemctl --user daemon-reload
systemctl --user start 'customer@001.service'
```

The instance drop-in assigns unique loopback ports. The template uses `%h` for
the service account's private data/config directories and `%i` for the
container name. For another customer, provision that customer's own service
account and data/config directories, add an instance symlink/drop-in, and
assign unused ports. The current pilot uses:

- web: `127.0.0.1:18001`;
- SSH: `127.0.0.1:22201`;
- data/config: `/home/svc-c001/data` and `/home/svc-c001/config`;
- limits: 1 CPU (`CPUQuota=100%` + `--cpus=1.0`), 1 GiB RAM, swap 0
  (`Memory=1g` + `--memory-swap=1g` + `MemorySwapMax=0`, reclaim from
  `MemoryHigh=900M`), 512 processes (`PidsLimit=512`, `TasksMax=1024`
  service-wide), fair-share `CPUWeight=100` / `IOWeight=100` /
  `--blkio-weight=100`. Pinned per instance in `20-resources.conf`
  (see customer-provisioning); re-run provision to pick up template changes.

Both ports are loopback-only. The image disables SSH password authentication;
add the customer's public key to `/home/customer/.ssh/authorized_keys` through
the provisioning process before granting access.

## PHP selection test

The pilot Nginx config maps hostnames to PHP-FPM listeners. With the service
running, verify each route from the host:

```sh
curl -H 'Host: php82.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php83.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php84.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php85.test' http://127.0.0.1:18001/index.php
curl -H 'Host: php86.test' http://127.0.0.1:18001/index.php
```

The response identifies the PHP version. To change a site's version, update
its `map` entry in the mounted Nginx config, then run `nginx -t` and reload
Nginx inside the container. The hostnames above are test values, not
production domains.

## Site directories

cPanel-style layout inside the mounted data directory:

- primary domain: `~/data/public_html`
- add-on domain: `~/data/<domain>`, e.g. `~/data/shop.example.com`

Nginx maps each `Host` to its directory via `$site_root` (default
`public_html`) and to its PHP version via `$php_fpm_backend`. Both maps live
in the mounted `~/config/nginx.conf`, so the control panel manages domains by
editing that file and reloading Nginx inside the container. No image rebuild
needed per site.

## Port apps (Node/Python)

Per-domain reverse proxies plus persistent processes, backed by host-side
`~/config` and in-container includes:

- `~/config/sites/<domain>.conf`: `server { server_name <domain>;
  location / { proxy_pass http://127.0.0.1:<3xxx>; ... } }`, included from
  `nginx.conf`. Exact `server_name` match takes precedence over the catch-all
  PHP server above.
- `~/config/supervisor/<domain>.conf`: `[program:app-<domain>]` running the
  panel-chosen command as `customer` in `/srv/customer/<domain>` (or
  `public_html` with `--primary`), included from `supervisord.conf`.

Manage them with `customer-apps/scripts/add-port-app.sh` /
`remove-port-app.sh` (port range `3000-3999`, panel-allocated). Rebuilding
the image and recreating the container is required once to pick up the new
`supervisord.conf` `[include]`; `add-port-app.sh` patches an older mounted
`nginx.conf` that lacks the sites include automatically.

## Not production-complete

PHP executables/configuration are mounted read-only from host runtime prefixes.
After updating the host RPMs, send a graceful reload to the affected
per-customer PHP-FPM master using the shared-runtime module's host helper.

The pilot does not yet provide XFS project quotas, host Nginx integration,
MariaDB connectivity, automated fleet-wide rollout/rollback, log rotation
policy, or host Nginx/MariaDB wiring and fleet-wide rollout/rollback. Disk
quota and data backup must be implemented before production use.
