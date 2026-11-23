# Architecture

## Service boundaries

```text
Internet
  |
  +-- Host Nginx: plain-HTTP reverse proxy (TLS terminates at the edge, outside this repo)
  |
  +-- Rootless Podman: one customer container per customer
  |     +-- SSH, cron, web gateway, and customer runtimes
  |     +-- Read-only mounts from host-managed runtime trees
  |
  +-- Host MariaDB: shared server, database/user per website
```

Host services and customer workloads have separate lifecycles:

- Host Nginx and MariaDB are not installed in customer images as servers.
- A customer image may contain its own internal web gateway and a MariaDB
  client, but it must not receive host Podman sockets, host credentials, or
  privileged host mounts.
- Shared runtimes may be mounted read-only from versioned host paths. Their
  packages are installed and patched centrally; application processes remain
  inside each customer's container and are gracefully reloaded per customer.
  Never mount writable host runtime directories into customer containers.
- Each customer gets a dedicated unprivileged host account that owns its
  rootless Podman storage and systemd user units. Customer SSH must terminate
  in that customer's container, not in a general-purpose host shell.
- CPU and memory limits use cgroups v2. Disk limits cover both customer
  data and container storage via XFS project quotas, one project per
  service account (see `host-storage`).
- Websites within one customer intentionally share a container and filesystem.
  Website-specific PHP-FPM pools and database credentials do not make websites
  security-isolated from one another.

## Module boundaries

| Module | Responsibility | Status |
| --- | --- | --- |
| `customer-container` | Customer image, runtime processes, and rootless Quadlet | Prototype |
| `shared-runtimes` | Host Remi PHP RPMs mounted read-only into customers | PHP prototype |
| `host-nginx` | Host customer/site reverse-proxy configuration (plain HTTP only) | Prototype scripts |
| `host-mariadb` | Shared DB service, per-site accounts, backup and restore | Prototype scripts |
| `customer-provisioning` | Create accounts, storage, limits, keys, and service units | Prototype scripts |
| `customer-apps` | Per-site mechanism (nginx maps, PHP version, data dirs) for the panel | Prototype scripts |
| `host-storage` | XFS project quotas per customer (soft 8G / hard 10G) | Enforcing |
| `monitoring-backup` | Resource monitoring, logs, and backup orchestration | Prototype scripts |

## Runtime selection

The customer container runs one PHP-FPM service for each installed PHP version.
Its internal Nginx maps each site hostname to the matching FPM listener. Host
Nginx should proxy to the customer's loopback-only web port; runtime choice
remains inside the customer module.

The PHP executables and extensions are host RPMs under Remi's parallel
`/opt/remi/php<minor>` paths. Updating a package changes the files available to
every container, but does not replace code already loaded by running FPM
processes. The rollout controller must gracefully signal the corresponding
FPM master in each customer container. Roll out in batches and health-check
each customer. Node.js and Python still ship in the image until equivalent
shared runtime bundles are implemented.

Runtime versions must be reviewed against upstream support windows before
production rollout. In particular, PHP 8.2 is near the end of security support
as of this prototype.
