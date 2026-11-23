# Host storage (XFS project quotas per customer)

One project per service account (`projid = account UID`, path = account home)
covers **both** `~/data` + `~/config` **and** rootless container storage
(`~/.local/share/containers`). Enforcement is at the filesystem level, so a
customer can never exceed their grant no matter which process writes.

## Activate once (reboot required)

XFS project accounting only turns on at the initial mount (remount is
silently ignored), so the script persists both `fstab` and `rootflags`:

```sh
sudo modules/host-storage/scripts/enable-project-quota.sh  # fstab + rootflags=pquota
sudo reboot
sudo modules/host-storage/scripts/enable-project-quota.sh  # verify ACTIVE
```

## Scripts (run as root)

```sh
# Cap a customer (default soft 8G / hard 10G):
sudo modules/host-storage/scripts/set-customer-quota.sh svc-c002
sudo modules/host-storage/scripts/set-customer-quota.sh svc-c002 --hard-gb 20 --soft-gb 16

# Usage vs limits (du fallback while accounting is off):
sudo modules/host-storage/scripts/quota-report.sh
```

`provision-customer.sh` accepts `--quota-gb N` (default 10) and applies the
hard limit with soft = 80% right after start; while accounting is off it warns
and the limits are applied automatically at boot after the reboot
(`systemd/sandpod-quotas.service` re-runs `reapply-quotas.sh` for every
registry customer with default 10/8G). Custom `--quota-gb` values are not
stored in the registry: re-run `set-customer-quota.sh` manually for those
accounts after the first pquota reboot.

## Boot re-apply (new installs + existing accounts)

Tags persist in the filesystem, but `limit -p` only sticks while `/` has
accounting. Install once so existing customers regain limits after reboot:

```sh
sudo cp modules/host-storage/systemd/* /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now sandpod-quotas.service
```

## Conventions

* Project name = service user, id = its UID - no extra allocator needed.
  Registries: `/etc/projid` (`<uid>:<user>`), `/etc/projects` (`<user>:<home>`).
* Quotas are whole GB; soft <= hard. Exceeding soft warns, exceeding hard
  fails writes (`EDQUOT`/`ENOSPC` to the customer processes).
* Inode limits default to unset (filesystem default, millions). Pass
  `--hard-inodes N --soft-inodes N` to `set-customer-quota.sh` (or
  `provision-customer.sh --hard-inodes/--soft-inodes`) when customers store
  millions of tiny files; applied as `ihard/isoft` in the same
  `xfs_quota limit -p` call.

## Status

Enforcing once `/` is mounted `pquota`/`prjquota` (see Activate above).
`set-customer-quota.sh` tags + limits in one run; provision applies it
automatically via `--quota-gb`; `systemd/sandpod-quotas.service` re-applies
limits for all registry customers at boot.
