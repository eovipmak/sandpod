# Customer container

This module builds the first rootless customer container prototype.

## Contents

- `Containerfile`: AlmaLinux 10 image and customer-side packages.
- `config/`: SSH, internal Nginx, and process-supervisor configuration.
- `quadlet/customer@.container`: reusable rootless Podman template.
- `quadlet/customer@001.container`: pilot instance symlink.
- `quadlet/customer@001.container.d/`: pilot-specific host ports.
- `examples/pilot-site/`: test HTML and PHP endpoints.

## Runtime matrix

| Runtime | Pilot version | Command / FPM listener |
| --- | --- | --- |
| PHP | 8.2 | `php82`, FPM `127.0.0.1:9082` |
| PHP | 8.3 | `php`, FPM `127.0.0.1:9000` |
| PHP | 8.4 | `php84`, FPM `127.0.0.1:9084` |
| Node.js | 22 | `node`, `npm` |
| Node.js | 24 | `node-24`, `npm-24` |
| Python | 3.13 | `python3.13` |

PHP 8.2 and 8.4 use Remi parallel-install packages; the image also enables
EPEL for tools such as `htop` and Python 3.13. Their signing keys are verified
by DNF. These repositories are used inside the image build, not added to the
host by this module.

## Build and run

Build the image **as the rootless service account**, from this directory:

```sh
podman build -t localhost/sandpod/customer-runtime:pilot .
```

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
```

The response identifies the PHP version. To change a site's version, update
its `map` entry in the mounted Nginx config, then run `nginx -t` and reload
Nginx inside the container. The hostnames above are test values, not
production domains.

## Not production-complete

The pilot does not yet provide XFS project quotas, host Nginx integration,
MariaDB connectivity, automated customer provisioning, log rotation policy,
or full runtime matrices (PHP 8.2/8.3/8.4, Node 20/22/24, Python
3.11/3.12/3.13). Disk quota and data/image backup must be implemented before
production use.
