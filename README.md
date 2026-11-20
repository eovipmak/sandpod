# SandPod

SandPod is a modular, rootless Podman-based hosting platform for isolating
customer workloads while sharing host-level Nginx and MariaDB services.

## Repository layout

```text
docs/                         Architecture and implementation notes
modules/
  customer-container/         Customer image, Quadlet, and pilot site
```

Each independently deployable feature belongs under `modules/` and owns its
build files, service definitions, configuration, tests, and operator
documentation. Shared design decisions belong in `docs/`.

## Current implementation

`modules/customer-container/` is a working single-customer prototype. It runs
rootless, has its own filesystem tree, and includes SSH, cron, an internal
Nginx gateway, PHP-FPM 8.2/8.3/8.4, Node.js 22/24, Python 3.13, and development
tools. Nginx selects a PHP-FPM version by hostname.

The prototype's web and SSH ports are loopback-only (`127.0.0.1:18001` and
`127.0.0.1:22201`). It is not yet connected to host Nginx or MariaDB, and it
does not yet enforce a disk quota.

See [the architecture notes](docs/architecture.md) and the
[customer container module guide](modules/customer-container/README.md).
