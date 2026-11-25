# Customer provisioning

Automates the manual steps from `customer-container/README.md`: one dedicated
unprivileged host account per customer, rootless Quadlet units, `~/data` +
`~/config`, loopback ports, SSH key injection, and a fleet registry.

## Dependencies

* `../customer-container/quadlet/customer@.container` - unit template (copied).
* `../customer-container/config/nginx.conf` - seed for `~/config/nginx.conf`.
* `../customer-container/config/default-index.html` + `examples/pilot-site/index.php` - seed `public_html/`.
* Image `localhost/sandpod/customer-runtime:node-shared` built beforehand
  (all customers use it; `:pilot` is retired).

## Scripts (run as root)

```sh
# New customer 002: auto ports (18002/22202), quota 10G, start immediately:
sudo sandpodctl provision-customer 002

# Pin ports + key:
sudo sandpodctl provision-customer 003 \
  --service-user svc-c003 --web-port 18003 --ssh-port 22203 \
  --ssh-key ~/.ssh/id_rsa.pub

# Verify (pilot map fixtures):
sandpodctl health-check 18002

# Add/rotate a key later (container /home/customer is ephemeral):
sudo sandpodctl add-ssh-key svc-c002 customer-002 ~/.ssh/id_rsa.pub

# Remove service, keep data (default). Add --delete-data / --delete-user to wipe:
sudo sandpodctl remove-customer 002
```

`sandpodctl provision-customer` does: `useradd -s /sbin/nologin` (reuse if exists) ->
`loginctl enable-linger` -> install `~/.config/containers/systemd/customer@.container`
+ symlink `customer@<id>.container` + `.container.d/10-instance.conf`
(`Image` + `PublishPort 127.0.0.1:<web>:80 / <ssh>:22`) -> `~/data/public_html`
+ `~/config/nginx.conf` (kept if exists) -> `systemctl --user daemon-reload +
restart/enable` -> optional `sandpodctl add-ssh-key` -> append `/etc/sandpod/customers.tsv`
(`instance, service_user, container, web_port, ssh_port, image`).

Resource caps: each customer is pinned in
`customer@<id>.container.d/20-resources.conf` (`--cpus 1.0` / `--memory 1g` /
`--io-weight 100` defaults): podman-level `Memory` + `PidsLimit=512` +
`PodmanArgs --memory-swap=<mem> --cpus`,
and systemd-level `CPUQuota` (= cpus x 100%) + `CPUWeight=100` + `MemoryHigh`
(90% of Max) + `MemoryMax` + `MemorySwapMax=0` + `TasksMax=1024` +
`IOWeight`. A busy customer is throttled/OOM-killed inside its own cgroup and
cannot starve neighbours. Re-run provision with new flags to change an
existing customer (the template copy and both drop-ins are rewritten).
Absolute IO bandwidth caps are intentionally not in the template (they need a
host device name); add them per host in a `30-io.conf` drop-in if needed.

Port auto-allocation: `18000+id` / `22200+id`, bumped past `ss` listeners and
registry entries (both web and ssh ports match the `\t<port>\t` field; the old
ssh `\t<port>$` check never matched because the image column follows).
Re-checked under the registry lock before writing. Use `--force` to reuse a
port `ss` sees. Registry writes (`provision` + `remove --delete-registry-entry`)
are serialized with `flock` on `<registry>.lock`; pass
`--registry <path>` to target a fixture TSV in repo tests
(`SANDPOD_USED_PORTS="18001,18002"` fakes busy ports without `ss`).
Quota: `--quota-gb N` (soft 80%) plus optional `--hard-inodes/--soft-inodes`
forwarded to `sandpodctl set-customer-quota`.

## Registry

Live mapping at `/etc/sandpod/customers.tsv` (see
`config/registry.tsv.example`). Fleet helpers (`shared-runtimes` reload,
future `monitoring-backup`) iterate this file as `service_user -> container`
source of truth - never probe containers.

Instance `001/svc-c001` predates this module; it can be (re)registered with
the normal idempotent provision command (it now runs `:node-shared`):

```sh
sudo sandpodctl provision-customer 001 --service-user svc-c001 \
  --web-port 18001 --ssh-port 22201
```

## Rollback

* Provision failed pre-start: re-run same command (idempotent), or
  `sandpodctl remove-customer <id>` then retry.
* Wrong ports/image: edit `<unit>.container.d/10-instance.conf`, re-run
  `sandpodctl provision-customer <id>` with corrected flags (rewrites drop-in,
  restarts unit).
* Full wipe: `sandpodctl remove-customer <id> --delete-data --delete-registry-entry`
  (add `--delete-user` to drop the account; requires `--delete-data`).

## Gotchas

* Service accounts use `/sbin/nologin` - host SSH always fails; customer SSH
  terminates in the container (`AllowUsers customer`). That is intentional.
* `/home/customer/.ssh/authorized_keys` lives in container storage, not in
  `~/data`. Re-apply keys after container recreate.
* Existing customers keep their `nginx.conf` on re-provision (only missing
  files are seeded). New customers get the current template
  (`root $site_root` → `public_html`, php82..php86 map, `sites/*.conf`).
  Compare/diff before migrating an old config via the control panel.
