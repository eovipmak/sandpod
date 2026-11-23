# sandpodctl — 1 binary Go thay toàn bộ `modules/*/scripts/*.sh`

Linux-first, dễ port OS khác sau: mọi gọi OS (runuser, podman, systemctl,
XFS, useradd, dnf, fnm/uv) nằm gọn trong `internal/platform`. Các package
còn lại chỉ dùng stdlib, không dependency ngoài.

## Layout (mỗi module 1 package riêng, build ra 1 binary)

```text
modules/sandpodctl/
  cmd/sandpodctl/main.go      # dispatcher: sandpodctl <command> [args...]
  internal/platform/          # runuser/podman/exec, validate, nginx map edit, registry lock
  internal/provisioning/      # provision/remove/ssh-key/health (customer-provisioning)
  internal/apps/              # add/remove/list/set-php/port-app + lib.sh (customer-apps)
  internal/runtimes/          # php/node/python install/update + reload (shared-runtimes)
  internal/nginx/             # generate-site-conf + reload (host-nginx)
  internal/mariadb/           # create/backup/restore (host-mariadb)
  internal/storage/           # quota enable/report/reapply/set (host-storage)
  internal/fleet/             # backup-customer, resource-report, rollout-php/app (monitoring-backup)
```

## Build / test

```sh
make -C modules/sandpodctl build   # -> modules/sandpodctl/sandpodctl
make -C modules/sandpodctl test    # go test ./...
make -C modules/sandpodctl vet
make -C modules/sandpodctl cross   # linux + darwin + windows compile check
```

## Tương thích args (panel-api không phải sửa)

Mỗi subcommand giữ nguyên argv của script `.sh` tương ứng, exit code giữ
nguyên (`0` ok, `1` lỗi runtime, `2` sai args). Ví dụ:

| Bash cũ | Go mới |
|---|---|
| `provision-customer.sh 002 ...` | `sandpodctl provision-customer 002 ...` |
| `add-site.sh u c d --php 8.3` | `sandpodctl add-site u c d --php 8.3` |
| `reload-customer-php.sh u c 8.3` | `sandpodctl reload-customer-php u c 8.3` |
| `rollout-php-patch.sh --batch-size 5` | `sandpodctl rollout-php-patch --batch-size 5` |

Đủ 31/31 lệnh (trừ `lib.sh` — đã thành `internal/apps/lib.go` dùng chung).

Busybox mode: gọi qua symlink tên `<cmd>[.sh]` cũng dispatch đúng, nên
migration có thể làm dần:

```sh
sudo make -C modules/sandpodctl shims  # /usr/local/bin/<cmd>{,.sh} -> sandpodctl
```

## Cross-OS

- Prod: AlmaLinux 10 (`GOOS=linux`). Các lệnh yêu cầu root + user manager
  như bash (`Run as root`, `enable-linger first`).
- Dev: build chạy được trên macOS/Windows để test validate/map logic
  (`go test`, `--help`, sai args → exit 2). Các lệnh cần `runuser/podman/
  systemctl/xfs_quota` sẽ báo lỗi rõ thay vì chạy nửa chừng.
- Muốn support OS mới: chỉ cần port `internal/platform` (exec, user lookup,
  flock, prefix guard) + thay `dnf`/`grubby`/`semanage` tương đương.
