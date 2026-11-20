#!/usr/bin/env bash
# Remove a site's nginx map entries (data kept by default). Run as root.
# Usage: remove-site.sh <service-user> <container> <domain> [--delete-data]
set -euo pipefail
[[ $# -ge 3 ]] || { echo "Usage: $0 <service-user> <container> <domain> [--delete-data]" >&2; exit 2; }
SERVICE_USER="$1"; CONTAINER="$2"; DOMAIN="${3,,}"; shift 3
DELETE_DATA=0
while [[ $# -gt 0 ]]; do case "$1" in
  --delete-data) DELETE_DATA=1; shift ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"
valid_domain "$DOMAIN" || { echo "Invalid domain: $DOMAIN" >&2; exit 2; }

cp -a "$NGINX_CONF" "${NGINX_CONF}.bak"
map_del site_root "$DOMAIN"
map_del php_fpm_backend "$DOMAIN"
if ! nginx_test_reload; then
    echo "Reload failed, restoring backup." >&2
    mv "${NGINX_CONF}.bak" "$NGINX_CONF"
    exit 1
fi
rm -f "${NGINX_CONF}.bak"
if [[ "$DELETE_DATA" -eq 1 && -d "${HOME_DIR}/data/${DOMAIN}" ]]; then
    rm -rf "${HOME_DIR}/data/${DOMAIN}"
    echo "Deleted data for ${DOMAIN}."
fi
echo "Removed site ${DOMAIN} (maps deleted)."
