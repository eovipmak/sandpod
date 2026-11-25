# Host Nginx (plain-HTTP reverse proxy)

Host Nginx is the only Internet-facing web endpoint. Customer containers stay
on loopback (`127.0.0.1:18xxx -> 80`); this module maps public domains to
those ports while preserving `Host` (the customer Nginx uses `$host` for
`$site_root` and `$php_fpm_backend` maps).

Scope: this module only proxies plain HTTP to customer loopback ports.
TLS termination and certificate issuance/renewal belong to the
edge/control-panel layer (outside this repo, see rule 6 in `docs/plan.md`)
and are never done here or inside customer containers. Behind the proxy all
customer traffic is plain HTTP on loopback; `X-Forwarded-Proto` is set (see
generated template) so apps can trust the original scheme.

## Layout

* `config/sites/customer-002.conf.example` - generated file example.
* `config/sandpod-logrotate.conf` - logrotate for `sandpod-*.log` (14 days) → `/etc/logrotate.d/sandpod`.
* `sandpodctl generate-site-conf` - registry + domain map -> `/etc/nginx/conf.d/sandpod-<id>.conf` (validates domains/ports, warns duplicates + stale; `--prune` deletes stale).
* `sandpodctl reload-host-nginx` - `nginx -t && systemctl reload nginx`.
* `systemd/sandpod-nginx-regen.{path,service}` - auto-regen + reload when `/etc/sandpod/*.tsv` change (`cp systemd/* /etc/systemd/system/ && systemctl enable --now sandpod-nginx-regen.path`).

## Inputs (policy lives outside this repo)

* `/etc/sandpod/customers.tsv` - written by `customer-provisioning`
  (`instance, service_user, container, web_port, ssh_port, image`).
* `/etc/sandpod/sites.tsv` - control panel owns this: one
  `<instance><TAB><domain>` per line. First domain is canonical, rest are
  `server_name` aliases.

```sh
printf '002\texample002.test\n' | sudo tee /etc/sandpod/sites.tsv
sudo sandpodctl generate-site-conf
sudo sandpodctl reload-host-nginx
curl -H 'Host: example002.test' http://127.0.0.1/
```

## Rollback / removal

Delete `/etc/nginx/conf.d/sandpod-<id>.conf`, reload. To take one customer
offline without touching others: `mv sandpod-002.conf ~root/ && reload`.

## Not handled here

TLS termination, certificate issuance/renewal (including ACME challenges),
rate limiting, WAF, per-site caching, and HTTP/3 are out of scope for this
repo - owned by the edge/control-panel team. Status: prototype scripts.
