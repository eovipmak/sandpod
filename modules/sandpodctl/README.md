# sandpodctl — 1 binary Go cho toàn bộ thao tác vận hành SandPod

Thay thế toàn bộ `modules/*/scripts/*.sh` (đã xóa). Linux-first, dễ port OS
khác sau: mọi gọi OS (runuser, podman, systemctl, XFS, useradd, dnf, fnm/uv,
SELinux) nằm trong `internal/platform`. Các package còn lại chỉ dùng stdlib.

## Layout (mỗi module 1 package, build ra 1 binary)

```text
modules/sandpodctl/
  cmd/sandpodctl/main.go      # dispatcher: sandpodctl <command> [args...]
  internal/platform/          # OS helpers dùng chung: exec, runuser (AsUser), podman,
                              #   SELinux, validate, nginx map edit, registry lock
  internal/provisioning/      # provision/remove/ssh-key/health (customer-provisioning)
  internal/apps/              # add/remove/list/set-php/port-app + lib (customer-apps)
  internal/runtimes/          # php/node/python install/update + reload (shared-runtimes)
  internal/nginx/             # generate-site-conf + reload (host-nginx)
  internal/mariadb/           # create/backup/restore (host-mariadb)
  internal/storage/           # quota enable/report/reapply/set (host-storage)
  internal/fleet/             # backup-customer, resource-report, rollout-php/app (monitoring-backup)
```

Quy tắc reuse: code cần chạy lệnh dưới tên tài khoản customer (`runuser` +
`XDG_RUNTIME_DIR`/`DBUS_SESSION_BUS_ADDRESS`) phải dùng
`platform.AsUser` / `RunAsUser` / `OutputAsUser` / `UserPodman`; tra user
dùng `platform.LookupAccount`; SELinux dùng `platform.SELinuxEnforcing`,
`LabelContainerFiles`, `Restorecon`. Không tự ghép `runuser` hay `id -u`
trong package khác.

## Build / test

```sh
make -C modules/sandpodctl build   # -> modules/sandpodctl/sandpodctl
make -C modules/sandpodctl test    # go test ./...
make -C modules/sandpodctl vet
make -C modules/sandpodctl cross   # linux + darwin + windows compile check
sudo make -C modules/sandpodctl install   # -> /usr/local/bin/sandpodctl
```

Đường dẫn trong systemd units và panel-api trỏ tới binary trong repo
(`/opt/sandpod/repo/modules/sandpodctl/sandpodctl`); panel-api tự tìm binary
theo thứ tự: `SANDPOD_SANDPODCTL` → repo build → `/usr/local/bin/sandpodctl`.

## Cách gọi

```sh
sudo sandpodctl provision-customer 002 ...
sudo sandpodctl add-site svc-c001 customer-001 shop.example.com --php 8.3
sudo sandpodctl reload-customer-php svc-c001 customer-001 8.3
sudo sandpodctl rollout-php-patch --batch-size 5
```

Argv và exit code giữ nguyên contract của script bash cũ: `0` ok, `1` lỗi
runtime, `2` sai args. `sandpodctl --help` liệt kê đủ lệnh.

Busybox mode: gọi qua symlink tên `<cmd>` (hoặc `<cmd>.sh`) cũng dispatch
đúng subcommand, nên symlink cũ trong `/usr/local/bin` vẫn chạy được.

## Cross-OS

- Prod: AlmaLinux 10 (`GOOS=linux`). Các lệnh yêu cầu root + user manager
  (`Run as root`, `enable-linger first`).
- Dev: build chạy được trên macOS/Windows để test validate/map logic
  (`go test`, `--help`, sai args → exit 2). Các lệnh cần `runuser/podman/
  systemctl/xfs_quota` sẽ báo lỗi rõ thay vì chạy nửa chừng.
- Muốn support OS mới: chỉ cần port `internal/platform` (exec, user lookup,
  flock, prefix guard, SELinux) + thay `dnf`/`grubby`/`semanage` tương đương.

## Rollback

Bash scripts đã bỏ hẳn; muốn quay lại bản cũ dùng `git checkout <commit-trước-refactor>`.
