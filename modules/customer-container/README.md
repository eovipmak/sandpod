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
| Node.js | 22 | `node` / `node-22`, `npm` |
| Node.js | 24 | `node-24`, `npm-24` |
| Node.js | 26 | `node-26`, `npm-26` |
| Python | 3.12 | `python3` / `python3.12` (system) |
| Python | 3.13 | `python3.13` |
| Python | 3.14 | `python3.14` |

PHP executables and extensions are not in the image. They come from the host's
read-only Remi SCL mounts, managed by the
[shared-runtimes module](../shared-runtimes/README.md). EPEL remains enabled
inside the image for tools such as `htop` and Python 3.13.

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
- limits: 1 CPU, 1 GiB RAM, 512 processes.

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
```

The response identifies the PHP version. To change a site's version, update
its `map` entry in the mounted Nginx config, then run `nginx -t` and reload
Nginx inside the container. The hostnames above are test values, not
production domains.

## Not production-complete

PHP executables/configuration are mounted read-only from host runtime prefixes.
After updating the host RPMs, send a graceful reload to the affected
per-customer PHP-FPM master using the shared-runtime module's host helper.

The pilot does not yet provide XFS project quotas, host Nginx integration,
MariaDB connectivity, automated fleet-wide rollout/rollback, log rotation
policy, or host Nginx/MariaDB wiring and fleet-wide rollout/rollback. Disk
quota and data backup must be implemented before production use.
