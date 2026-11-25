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
| `shared-runtimes` | Host Remi PHP RPMs + fnm-managed Node trees + standalone Python prefixes, mounted read-only into customers | Prototype |
| `host-nginx` | Host customer/site reverse-proxy configuration (plain HTTP only) | Prototype |
| `host-mariadb` | Shared DB service, per-site accounts, backup and restore | Prototype |
| `customer-provisioning` | Create accounts, storage, limits, keys, and service units | Prototype |
| `customer-apps` | Per-site mechanism (nginx maps, PHP version, data dirs) for the panel | Prototype |
| `host-storage` | XFS project quotas per customer (soft 8G / hard 10G) | Enforcing |
| `monitoring-backup` | Resource monitoring, logs, and backup orchestration | Prototype |
| `panel-api` | FastAPI daemon wrapping scripts for the panel (Spec v4, `openapi.yaml`): as root, loopback-only `127.0.0.1:8741`, Bearer token file `0600`, sync (<5s) + async (`202`+`jobId`) | Prototype |

## Runtime selection

The customer container runs one PHP-FPM service for each installed PHP version.
Its internal Nginx maps each site hostname to the matching FPM listener. Host
Nginx should proxy to the customer's loopback-only web port; runtime choice
remains inside the customer module.

The PHP executables and extensions are host RPMs under Remi's parallel
`/opt/remi/php<minor>` paths. Node.js (`22/24/26`, fnm-managed with stable
`/opt/sandpod/node/<major>` symlinks) and Python (`3.12/3.13/3.14`,
self-contained `/opt/sandpod/python/<ver>` prefixes) are host trees mounted
read-only the same way. Updating a package/tree changes the files available to
every container, but does not replace code already loaded by running
processes. The rollout controller must gracefully signal the corresponding
master in each customer container (FPM `USR2` per minor; port-app
`supervisorctl restart` per domain). Roll out in batches and health-check
each customer. Node/Python image RPMs remain only as fallback for containers
started without the shared mounts.

Runtime versions must be reviewed against upstream support windows before
production rollout. In particular, PHP 8.2 is near the end of security support
as of this prototype.
