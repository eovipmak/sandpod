# Customer apps (per-site mechanism for the control panel)

Host-side helpers that implement the panel contract in
[`docs/control-panel.md`](../../docs/control-panel.md): site directories,
nginx `$host` maps, and container reloads. The panel decides *which*
domains/versions/ports; these scripts execute *how* (idempotent, with
`nginx -t` gate and automatic rollback to `.bak` on failure).

## Scripts (run as root)

```sh
# PHP site on 002, served from ~/data/shop.example.com, PHP 8.4:
sudo sandpodctl add-site svc-c002 customer-002 shop.example.com --php 8.4
curl -H 'Host: shop.example.com' http://127.0.0.1:18002/index.php

# Primary domain (serves ~/data/public_html):
sudo sandpodctl add-site svc-c001 customer-001 example.com --primary --php 8.3

# Change version / remove / inspect:
sudo sandpodctl set-php-version svc-c002 customer-002 shop.example.com 8.5
sudo sandpodctl list-sites svc-c002 customer-002 [--json]
sudo sandpodctl remove-site svc-c002 customer-002 shop.example.com [--delete-data]
```

`sandpodctl add-site` creates `~/data/<domain>` (+ placeholder `index.php`),
inserts `$site_root` and `$php_fpm_backend` map entries, then
`nginx -t && nginx -s reload` inside the container via `podman exec`.
If the domain currently has a port app, its `sites/` + `supervisor/` configs
are deleted first (conversion back to PHP). Every write keeps a `.bak` and
restores it if the reload fails.

## Port apps (Node/Python)

```sh
sudo sandpodctl add-port-app svc-c002 customer-002 node.example.com --port 3001 --command "node server.js"
curl -H 'Host: node.example.com' http://127.0.0.1:18002/

sudo sandpodctl remove-port-app svc-c002 customer-002 node.example.com [--delete-data]
```

`sandpodctl add-port-app` writes `~/config/sites/<domain>.conf` (a `server` block with
exact `server_name` that shadows the catch-all PHP server and proxies `/` to
`http://127.0.0.1:<port>` with `Host` + `X-Forwarded-*` headers) and
`~/config/supervisor/<domain>.conf` (`[program:app-<domain>]` running
`<command>` as `customer` in `/srv/customer/<domain>`), drops any PHP map
entries for the domain, then `nginx -t && nginx -s reload` plus
`supervisorctl reread && supervisorctl update`. Rollback restores `.bak`
copies on failure. `sandpodctl remove-port-app` deletes both generated files and
reloads both services; re-add via `sandpodctl add-site` to convert back to PHP.
`sandpodctl list-sites` prints both the PHP maps and a `port_apps:` section.

## Contract notes

* Map values: `site_root` is `/srv/customer/<domain>` (or `public_html`
  for `--primary`); backend is the fixed FPM listener for the minor
  (8.2→`9082`, 8.3→`9000`, 8.4→`9084`, 8.5→`9085`, 8.6→`9086`).
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
`sandpodctl provision-customer` for new customers). `sandpodctl add-port-app` also patches a
pre-existing `~/config/nginx.conf` that lacks the sites include, but the
container must be recreated from the new image for the supervisord include
to take effect.
