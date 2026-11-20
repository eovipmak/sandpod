# SandPod

SandPod is a modular, rootless Podman-based hosting platform for isolating
customer workloads while sharing host-level Nginx and MariaDB services.

## Repository layout

```text
docs/                         Architecture and implementation notes
modules/
  customer-container/         Customer image, Quadlet, and pilot site
  shared-runtimes/            Host-managed read-only runtimes
  customer-provisioning/      Accounts, Quadlet units, ports, keys, registry
  customer-apps/              Per-site nginx maps and PHP versions (panel mechanism)
  host-nginx/                 Host TLS and reverse-proxy configuration
  host-mariadb/               Shared DB service, per-site accounts, backups
  host-storage/               XFS project quotas per customer
  monitoring-backup/          Fleet rollout, backups, resource reports
```

Each independently deployable feature belongs under `modules/` and owns its
build files, service definitions, configuration, tests, and operator
documentation. Shared design decisions belong in `docs/`.

## Current implementation

`modules/customer-container/` is a working single-customer prototype based on
the reusable `customer@.container` Quadlet template. It runs rootless, has its
own filesystem tree, and includes SSH, cron, an internal Nginx gateway,
PHP-FPM 8.2/8.3/8.4/8.5/8.6, Node.js 22/24/26, Python 3.12/3.13/3.14, and
development tools. Nginx selects a PHP-FPM version by hostname.

PHP binaries/extensions are host-managed read-only mounts; each customer still
runs its own PHP-FPM processes inside its rootless container. See
[`modules/shared-runtimes/`](modules/shared-runtimes/README.md).

Customers are provisioned by [`modules/customer-provisioning/`](modules/customer-provisioning/README.md)
(dedicated account, loopback ports `127.0.0.1:18xxx/222xx`, `~/data` +
`~/config`, fleet registry). Disk quotas are enforced per account via XFS
projects (soft 8G / hard 10G, see
[`modules/host-storage/`](modules/host-storage/README.md)). Host Nginx and
MariaDB integration exists as prototype scripts
([`host-nginx`](modules/host-nginx/README.md),
[`host-mariadb`](modules/host-mariadb/README.md)) with no production domains,
TLS certificates, or site databases yet.

See [the architecture notes](docs/architecture.md) and the
[customer container module guide](modules/customer-container/README.md).
