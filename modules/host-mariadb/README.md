# Host MariaDB (shared server, database/user per website)

One host MariaDB serves all sites. Each website gets its own
`DATABASE + USER` (least privilege); websites never share credentials and
never get `SUPER`/`ALL PRIVILEGES`.

## Container connectivity

Inside a rootless (pasta) container, `127.0.0.1` is the container itself.
Containers reach the host DB via **`host.containers.internal`**. Host side —
template `config/sandpod.cnf` (install: `cp config/sandpod.cnf /etc/my.cnf.d/`
+ `systemctl restart mariadb`; add the pasta gateway IP to `bind-address`):

```sh
podman exec customer-001 getent hosts host.containers.internal  # find gateway
```

Firewall (operator, on host — example for firewalld; never `0.0.0.0/0`):

```sh
sudo firewall-cmd --permanent --add-rich-rule='rule family=ipv4 source address=<pasta-subnet> port port=3306 protocol=tcp accept'
sudo firewall-cmd --reload
```

Users are created as `'user'@'%'` so the container netns IP is
accepted; tighten to the pasta subnet once it is pinned down in this
environment. `REQUIRE SSL` stays off until the panel owns cert distribution
(out of scope here).

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
  (see `monitoring-backup/systemd/`); retain 7 daily + 4 weekly, test-restore monthly.
  Schedule per DB (names are panel-owned):
  ```sh
  sudo cp modules/host-mariadb/systemd/* /etc/systemd/system/
  sudo systemctl daemon-reload
  sudo systemctl enable --now 'sandpod-db-backup@shop_db.timer'
  ```
* Dumps go to `/var/backups/sandpod` (`0700`, integrity-checked with
  `gzip -t`); `restore-site-db.sh` validates the dump, backs up the live DB
  first, and creates a missing DB. Sync off-host.

## Status

Prototype scripts only. MariaDB is installed but `inactive (dead)` on this
host; no site DBs created yet and container->host DB path is untested.
