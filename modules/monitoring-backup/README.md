# Monitoring + backup (fleet operations)

## Scripts (run as root)

* `scripts/rollout-php-patch.sh` - fleet PHP patch: `update-php-runtimes.sh`
  once, then per-customer per-minor `reload-customer-php.sh` + `health-check.sh`
  in batches (default 5, sleep 10s). Stops on first failed batch.
  ```sh
  sudo modules/monitoring-backup/scripts/rollout-php-patch.sh --batch-size 2
  sudo modules/monitoring-backup/scripts/rollout-php-patch.sh --minor 8.3 --minor 8.4 --skip-update
  ```
* `scripts/backup-customer.sh` - `tar` of `~/data` + `~/config` to
  `/var/backups/sandpod/<id>-<ts>.tar.gz`, keeps 7. Pair with
  `host-mariadb/scripts/backup-site-db.sh` for a full site backup.
* `scripts/resource-report.sh` - `podman stats` per registry customer.

## Log policy

* Customer Nginx/FPM/cron/sshd log to container stdout/stderr via supervisord
  (`/dev/fd/1`, `/proc/self/fd/2`) - collected by `podman logs` / journald of
  the owning user manager. No files to rotate inside containers by design.
* Host nginx: default `/var/log/nginx/*` with system `logrotate`. Keep 14 days.
* Retention: 7 daily customer tars + DB dumps; sync `/var/backups/sandpod` off-host.

## Not enforced yet (before production)

* Disk quota: enforced by the `host-storage` module (XFS project per account,
  10G default). Active only after the `pquota` reboot; `resource-report.sh`
  falls back to `du` until then.
* Alerting: no CPU/RAM/disk alerts, no dead-container auto-notify. Next step is
  a systemd timer running `resource-report.sh` + `health-check.sh` per customer
  and paging on failure.
