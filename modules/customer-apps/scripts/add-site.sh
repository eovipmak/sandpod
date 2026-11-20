#!/usr/bin/env bash
# Add (or update) a PHP site: data dir + nginx map entries + container reload.
# Run as root. Idempotent.
# Usage: add-site.sh <service-user> <container> <domain> [--php 8.3] [--primary]
#   --primary maps the domain to ~/data/public_html; default maps to ~/data/<domain>.
set -euo pipefail

[[ $# -ge 3 ]] || { echo "Usage: $0 <service-user> <container> <domain> [--php 8.3] [--primary]" >&2; exit 2; }
SERVICE_USER="$1"; CONTAINER="$2"; DOMAIN="${3,,}"; shift 3
PHP_MINOR="8.3"; PRIMARY=0
while [[ $# -gt 0 ]]; do case "$1" in
  --php) PHP_MINOR="${2:-}"; shift 2 ;;
  --primary) PRIMARY=1; shift ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done

[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"

valid_domain "$DOMAIN" || { echo "Invalid domain: $DOMAIN" >&2; exit 2; }
valid_minor "$PHP_MINOR" || { echo "Unsupported PHP minor (8.2-8.6): $PHP_MINOR" >&2; exit 2; }
eol_warn_for_minor "$PHP_MINOR"
BACKEND="$(backend_for_minor "$PHP_MINOR")"

if [[ "$PRIMARY" -eq 1 ]]; then
    SITE_ROOT="/srv/customer/public_html"
else
    install -d -m 0755 "${HOME_DIR}/data/${DOMAIN}"
    chown "${SERVICE_USER}:${SERVICE_USER}" "${HOME_DIR}/data/${DOMAIN}"
    SITE_ROOT="/srv/customer/${DOMAIN}"
    if [[ ! -f "${HOME_DIR}/data/${DOMAIN}/index.php" ]]; then
        printf '<?php\necho "OK from %s\\n";\n' "$DOMAIN" > "${HOME_DIR}/data/${DOMAIN}/index.php"
        chown "${SERVICE_USER}:${SERVICE_USER}" "${HOME_DIR}/data/${DOMAIN}/index.php"
    fi
fi

cp -a "$NGINX_CONF" "${NGINX_CONF}.bak"
# A PHP map entry is shadowed by an exact-match port-app server block, so a
# conversion back to PHP must delete the port-app configs first.
PORT_CONFLICT=0
[[ -f "${SITES_DIR}/${DOMAIN}.conf" ]] && PORT_CONFLICT=1 || true
[[ -f "${SUPERVISOR_DIR}/${DOMAIN}.conf" ]] && PORT_CONFLICT=1 || true
if [[ "$PORT_CONFLICT" -eq 1 ]]; then
    rm -f "${SITES_DIR}/${DOMAIN}.conf" "${SUPERVISOR_DIR}/${DOMAIN}.conf"
fi
map_set site_root "$DOMAIN" "$SITE_ROOT"
map_set php_fpm_backend "$DOMAIN" "$BACKEND"
if ! nginx_test_reload; then
    echo "Reload failed, restoring backup." >&2
    mv "${NGINX_CONF}.bak" "$NGINX_CONF"
    exit 1
fi
if [[ "$PORT_CONFLICT" -eq 1 ]]; then
    supervisor_reread_update || { echo "WARNING: nginx reloaded but supervisor update failed." >&2; exit 1; }
fi
rm -f "${NGINX_CONF}.bak"
echo "Site ${DOMAIN}: root=${SITE_ROOT} php=${PHP_MINOR} (${BACKEND})"
