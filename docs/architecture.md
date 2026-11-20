# Architecture

## Service boundaries

```text
Internet
  |
  +-- Host Nginx: TLS termination and reverse proxy
  |
  +-- Rootless Podman: one customer container per customer
  |     +-- SSH, cron, web gateway, and customer runtimes
  |
  +-- Host MariaDB: shared server, database/user per website
```

Host services and customer workloads have separate lifecycles:

- Host Nginx and MariaDB are not installed in customer images as servers.
- A customer image may contain its own internal web gateway and a MariaDB
  client, but it must not receive host Podman sockets, host credentials, or
  privileged host mounts.
- Each customer gets a dedicated unprivileged host account that owns its
  rootless Podman storage and systemd user units. Customer SSH must terminate
  in that customer's container, not in a general-purpose host shell.
- CPU and memory limits use cgroups v2. Disk limits must cover both customer
  data and container storage; the prototype still needs XFS project quotas.
- Websites within one customer intentionally share a container and filesystem.
  Website-specific PHP-FPM pools and database credentials do not make websites
  security-isolated from one another.

## Module boundaries

| Module | Responsibility | Status |
| --- | --- | --- |
| `customer-container` | Customer image, runtime processes, and rootless Quadlet | Prototype |
| `host-nginx` | Host TLS and customer/site reverse-proxy configuration | Planned |
| `host-mariadb` | Shared DB service, per-site accounts, backup and restore | Planned |
| `customer-provisioning` | Create accounts, storage, limits, keys, and service units | Planned |
| `monitoring-backup` | Resource monitoring, logs, and backup orchestration | Planned |

## Runtime selection

The customer container runs one PHP-FPM service for each installed PHP version.
Its internal Nginx maps each site hostname to the matching FPM listener. Host
Nginx should proxy to the customer's loopback-only web port; runtime choice
remains inside the customer module.

Runtime versions must be reviewed against upstream support windows before
production rollout. In particular, PHP 8.2 is near the end of security support
as of this prototype.
