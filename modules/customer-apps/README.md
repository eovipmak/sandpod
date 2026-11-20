# Customer apps (per-site mechanism for the control panel)

Host-side helpers that implement the panel contract in
[`docs/control-panel.md`](../../docs/control-panel.md): site directories,
nginx `$host` maps, and container reloads. The panel decides *which*
domains/versions/ports; these scripts execute *how* (idempotent, with
`nginx -t` gate and automatic rollback to `.bak` on failure).

## Scripts (run as root)

```sh
# PHP site on 002, served from ~/data/shop.example.com, PHP 8.4:
sudo modules/customer-apps/scripts/add-site.sh svc-c002 customer-002 shop.example.com --php 8.4
curl -H 'Host: shop.example.com' http://127.0.0.1:18002/index.php

# Primary domain (serves ~/data/public_html):
sudo modules/customer-apps/scripts/add-site.sh svc-c001 customer-001 example.com --primary --php 8.3

# Change version / remove / inspect:
sudo modules/customer-apps/scripts/set-php-version.sh svc-c002 customer-002 shop.example.com 8.5
sudo modules/customer-apps/scripts/list-sites.sh svc-c002 customer-002
sudo modules/customer-apps/scripts/remove-site.sh svc-c002 customer-002 shop.example.com [--delete-data]
```

`add-site.sh` creates `~/data/<domain>` (+ placeholder `index.php`),
inserts `$site_root` and `$php_fpm_backend` map entries, then
`nginx -t && nginx -s reload` inside the container via `podman exec`.
If the domain currently has a port app, its `sites/` + `supervisor/` configs
are deleted first (conversion back to PHP). Every write keeps a `.bak` and
restores it if the reload fails.

## Port apps (Node/Python)

```sh
sudo modules/customer-apps/scripts/add-port-app.sh svc-c002 customer-002 node.example.com --port 3001 --command "node server.js"
curl -H 'Host: node.example.com' http://127.0.0.1:18002/

sudo modules/customer-apps/scripts/remove-port-app.sh svc-c002 customer-002 node.example.com [--delete-data]
```

`add-port-app.sh` writes `~/config/sites/<domain>.conf` (a `server` block with
exact `server_name` that shadows the catch-all PHP server and proxies `/` to
`http://127.0.0.1:<port>` with `Host` + `X-Forwarded-*` headers) and
`~/config/supervisor/<domain>.conf` (`[program:app-<domain>]` running
`<command>` as `customer` in `/srv/customer/<domain>`), drops any PHP map
entries for the domain, then `nginx -t && nginx -s reload` plus
`supervisorctl reread && supervisorctl update`. Rollback restores `.bak`
copies on failure. `remove-port-app.sh` deletes both generated files and
reloads both services; re-add via `add-site.sh` to convert back to PHP.
`list-sites.sh` prints both the PHP maps and a `port_apps:` section.

## Contract notes

* Map values: `site_root` is `/srv/customer/<domain>` (or `public_html`
  for `--primary`); backend is the fixed FPM listener for the minor
  (8.2→`9082`, 8.3→`9000`, 8.4→`9084`, 8.5→`9085`).
* Port apps: one loopback port per app from `3000-3999`, allocated by the
  panel (scripts validate, never invent). The app itself must listen on
  `127.0.0.1:<port>`; only the container's Nginx (port 80) is reachable
  from outside. `command` runs with no shell unless wrapped explicitly
  (e.g. `--command "/bin/sh -l -c '...'"`), as `customer`.
* The panel remains the source of truth for domains and versions; these
  scripts never invent them and never probe the container for policy.

## Image requirements

Port apps need image build ≥ this change: `nginx.conf` includes
`include /etc/sandpod-config/sites/*.conf;`, `supervisord.conf` has the
`[include] files = /etc/sandpod-config/supervisor/*.conf` section, and
`~/config/sites` + `~/config/supervisor` exist (created by
`provision-customer.sh` for new customers). `add-port-app.sh` also patches a
pre-existing `~/config/nginx.conf` that lacks the sites include, but the
container must be recreated from the new image for the supervisord include
to take effect.
