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
Every write keeps a `.bak` and restores it if the reload fails.

## Contract notes

* Map values: `site_root` is `/srv/customer/<domain>` (or `public_html`
  for `--primary`); backend is the fixed FPM listener for the minor
  (8.2→`9082`, 8.3→`9000`, 8.4→`9084`, 8.5→`9085`).
* The panel remains the source of truth for domains and versions; these
  scripts never invent them and never probe the container for policy.

## Not yet (needs an image change)

* Node/Python port apps: the current `server` block has no proxy path, so
  `proxy_pass` per `Host` cannot be expressed in the two maps. Plan: image
  gains `include /etc/sandpod-config/sites/*.conf;` (+ panel-written
  per-domain server blocks) and supervisord gains
  `[include] files = /etc/sandpod-config/supervisor/*.conf` for persistent
  app processes, both backed by host-side `~/config`. Until then, port apps
  stay panel-managed experiments via `podman exec`, not a supported path.
