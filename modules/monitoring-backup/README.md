# Monitoring + backup (fleet operations)

## Scripts (run as root)

* `scripts/rollout-php-patch.sh` - fleet PHP patch: `update-php-runtimes.sh`
  once, then per-customer per-minor `reload-customer-php.sh` + `health-check.sh`
  in batches (default 5, sleep 10s). Stops on first failed batch.
  ```sh
  sudo modules/monitoring-backup/scripts/rollout-php-patch.sh --batch-size 2
  sudo modules/monitoring-backup/scripts/rollout-php-patch.sh --minor 8.3 --minor 8.4 --skip-update
  ```
* `scripts/rollout-app-patch.sh` - fleet Node/Python patch:
  `update-node-runtimes.sh` + `update-python-runtimes.sh` once, then per
  customer restart every port app (`reload-customer-app.sh` per domain from
  `list-sites.sh --json`) + `health-check.sh` in batches. Same batch/stop
  semantics as the PHP rollout. `--skip-node-update` / `--skip-python-update`
  / `--skip-update` re-run restarts without touching host trees.
  ```sh
  sudo modules/monitoring-backup/scripts/rollout-app-patch.sh --batch-size 2
  sudo modules/monitoring-backup/scripts/rollout-app-patch.sh --skip-python-update
  ```
* `scripts/backup-customer.sh` - `tar` of `~/data` + `~/config` to
  `/var/backups/sandpod/<id>-<ts>.tar.gz`, keeps 7. Pair with
  `host-mariadb/scripts/backup-site-db.sh` for a full site backup.
* `scripts/resource-report.sh` - `podman stats` per registry customer plus
  `DISK` (`report -p -b -h`: used/soft/hard) and `INODES` (`report -p -i -h`)
  columns joined by exact project name (== service user), fallback by numeric
  project id (== account UID). `quota-off`/`-` while accounting is off.
  `--json` prints a panel-parseable array (same convention as
  `customer-apps/scripts/list-sites.sh --json`). `--xfs-report-file` /
  `--xfs-inode-report-file` replay saved reports (repo-test hooks, offline use).
  Exit 1 when the registry is missing, 2 on bad arguments.

## Log policy

* Customer Nginx/FPM/cron/sshd log to container stdout/stderr via supervisord
  (`/dev/fd/1`, `/proc/self/fd/2`) - collected by `podman logs` / journald of
  the owning user manager. No files to rotate inside containers by design.
* Host nginx: default `/var/log/nginx/*` with system `logrotate`. Keep 14 days
  (template: `../host-nginx/config/sandpod-logrotate.conf` →
  `/etc/logrotate.d/sandpod`).
* Retention: 7 daily customer tars + DB dumps; sync `/var/backups/sandpod` off-host.

## Systemd scheduling (templates, operator installs on host)

`systemd/` holds templates (repo checkout at `/opt/sandpod/repo`, same
convention as `sandpod-panel-api.service`). They only RUN the mechanism —
retention count stays in the scripts, off-host sync and paging stay outside:

```sh
sudo cp modules/monitoring-backup/systemd/* /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now 'sandpod-backup@001.timer'  # per customer, daily
sudo systemctl enable --now sandpod-resource-report.timer  # fleet, hourly
journalctl -u sandpod-resource-report.service --since today  # JSON output
```

## Before production (policy-owned, ngoài scope repo)

* Disk quota: mechanism enforced by `host-storage` (XFS project per account,
  soft 8G/hard 10G default). Active only after the `pquota` reboot;
  `resource-report.sh` falls back to `du` until then. Retention/off-host sync
  thuộc ops/panel (ngoài scope `docs/scope.md` §3).
* Alerting/paging: repo chỉ xuất dữ liệu (`resource-report --json` +
  `health-check.sh`, hoặc `GET /v1/.../resources` qua panel-api);
  `sandpod-resource-report.timer` (hourly, xem `systemd/` ở trên) chỉ chạy
  cơ chế — ngưỡng page do ops/panel sở hữu.
