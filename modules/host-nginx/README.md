# Host Nginx (TLS termination + reverse proxy)

Host Nginx is the only Internet-facing web endpoint. Customer containers stay
on loopback (`127.0.0.1:18xxx -> 80`); this module maps public domains to
those ports while preserving `Host` (the customer Nginx uses `$host` for
`$site_root` and `$php_fpm_backend` maps).

## Layout

* `config/sites/customer-002.conf.example` - generated file example.
* `scripts/generate-site-conf.sh` - registry + domain map -> `/etc/nginx/conf.d/sandpod-<id>.conf`.
* `scripts/reload-host-nginx.sh` - `nginx -t && systemctl reload nginx`.

## Inputs (policy lives outside this repo)

* `/etc/sandpod/customers.tsv` - written by `customer-provisioning`
  (`instance, service_user, container, web_port, ssh_port, image`).
* `/etc/sandpod/sites.tsv` - control panel owns this: one
  `<instance><TAB><domain>` per line. First domain is canonical, rest are
  `server_name` aliases.

```sh
printf '002\texample002.test\n' | sudo tee /etc/sandpod/sites.tsv
sudo modules/host-nginx/scripts/generate-site-conf.sh
sudo modules/host-nginx/scripts/reload-host-nginx.sh
curl -H 'Host: example002.test' http://127.0.0.1/
```

## TLS

Terminated here, never in customer containers. Per site after HTTP works:

```sh
sudo dnf install -y certbot python3-certbot-nginx
sudo certbot --nginx -d example002.test -d www.example002.test
```

`/.well-known/acme-challenge/` stays on HTTP in every generated block so
renewals keep working. Behind the proxy all customer traffic is plain HTTP
on loopback; set `X-Forwarded-Proto` (already in template) and make apps
trust it.

## Rollback / removal

Delete `/etc/nginx/conf.d/sandpod-<id>.conf`, reload. To take one customer
offline without touching others: `mv sandpod-002.conf ~root/ && reload`.

## Not handled here

Rate limiting, WAF, per-site caching, and HTTP/3 are out of scope for this
repo - owned by another team. Status: prototype scripts, no TLS
certs provisioned yet in this environment.
