#!/usr/bin/env bash
# Change one site's PHP version (or the default): map edit + container reload.
# Run as root. Idempotent.
# Usage: set-php-version.sh <service-user> <container> <domain|default> <minor>
set -euo pipefail
[[ $# -eq 4 ]] || { echo "Usage: $0 <service-user> <container> <domain|default> <8.2|8.3|8.4|8.5>" >&2; exit 2; }
SERVICE_USER="$1"; CONTAINER="$2"; DOMAIN="${3,,}"; MINOR="$4"
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"

if [[ "$DOMAIN" != "default" ]]; then
    valid_domain "$DOMAIN" || { echo "Invalid domain: $DOMAIN" >&2; exit 2; }
    if [[ -f "${SITES_DIR}/${DOMAIN}.conf" || -f "${SUPERVISOR_DIR}/${DOMAIN}.conf" ]]; then
        echo "${DOMAIN} is a port app (proxy_pass); PHP version does not apply." >&2
        echo "Use remove-port-app.sh to drop it or add-site.sh to convert it to PHP." >&2
        exit 1
    fi
fi
valid_minor "$MINOR" || { echo "Unsupported PHP minor (8.2-8.5): $MINOR" >&2; exit 2; }
BACKEND="$(backend_for_minor "$MINOR")"

cp -a "$NGINX_CONF" "${NGINX_CONF}.bak"
map_set php_fpm_backend "$DOMAIN" "$BACKEND"
if ! nginx_test_reload; then
    echo "Reload failed, restoring backup." >&2
    mv "${NGINX_CONF}.bak" "$NGINX_CONF"
    exit 1
fi
rm -f "${NGINX_CONF}.bak"
echo "${DOMAIN}: php=${MINOR} (${BACKEND})"
