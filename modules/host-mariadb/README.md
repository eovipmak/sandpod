# Host MariaDB (shared server, database/user per website)

One host MariaDB serves all sites. Each website gets its own
`DATABASE + USER` (least privilege); websites never share credentials and
never get `SUPER`/`ALL PRIVILEGES`.

## Container connectivity

Inside a rootless (pasta) container, `127.0.0.1` is the container itself.
Containers reach the host DB via **`host.containers.internal`**. Host side:

```ini
# /etc/my.cnf.d/sandpod.cnf
[mariadbd]
bind-address = 127.0.0.1,::1
# plus the pasta gateway reachable from customer netns (verify with
# podman exec customer-001 getent hosts host.containers.internal).
```

Create a host firewall opening for the customer subnet only, never `0.0.0.0/0`
for 3306. Users are created as `'user'@'%'` so the container netns IP is
accepted; tighten to the pasta subnet once it is pinned down in this
environment.

## Scripts (run as root)

```sh
sudo systemctl enable --now mariadb
sudo modules/host-mariadb/scripts/create-site-db.sh shop_db shop_user
# -> prints DB_HOST/DB_NAME/DB_USER/DB_PASS once; panel vaults it.

sudo modules/host-mariadb/scripts/backup-site-db.sh shop_db
sudo modules/host-mariadb/scripts/restore-site-db.sh shop_db /var/backups/sandpod/shop_db-<ts>.sql.gz
```

Grants are limited to one database (`SELECT/INSERT/UPDATE/DELETE/CREATE/
DROP/INDEX/ALTER/...`). No cross-site access by construction.

## Backup policy (minimum before production)

* Nightly `backup-site-db.sh` per site + `tar` of `~/data` + `~/config`
  (see future `monitoring-backup`); retain 7 daily + 4 weekly, test-restore monthly.
* Dumps go to `/var/backups/sandpod` (`0700`); sync off-host.

## Status

Prototype scripts only. MariaDB is installed but `inactive (dead)` on this
host; no site DBs created yet and container->host DB path is untested.
