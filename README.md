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
  host-nginx/                 Host reverse-proxy configuration (plain HTTP; TLS owned by edge/panel)
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
multi-version PHP-FPM, Node.js, Python, and development tools.
Nginx selects a PHP-FPM version by hostname.

Runtime binaries/extensions are host-managed read-only mounts; each customer still
runs its own processes inside its rootless container. See
[`modules/shared-runtimes/`](modules/shared-runtimes/README.md).

Customers are provisioned by [`modules/customer-provisioning/`](modules/customer-provisioning/README.md)
(dedicated account, loopback ports `127.0.0.1:18xxx/222xx`, `~/data` +
`~/config`, fleet registry). Disk quotas are enforced per account via XFS
projects (soft 8G / hard 10G, see
[`modules/host-storage/`](modules/host-storage/README.md)). Host Nginx and
MariaDB integration exists as prototype scripts
([`host-nginx`](modules/host-nginx/README.md),
[`host-mariadb`](modules/host-mariadb/README.md)) with no production domains,
TLS termination/certificates (edge-owned, out of scope), or site databases yet.

See [the architecture notes](docs/architecture.md) and the
[customer container module guide](modules/customer-container/README.md).
