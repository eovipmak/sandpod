#!/usr/bin/env bash
# Remove a port app's nginx + supervisord configs (data kept by default).
# Run as root. No-op if the domain has no port app.
# Usage: remove-port-app.sh <service-user> <container> <domain> [--delete-data]
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

SITE_CONF="${SITES_DIR}/${DOMAIN}.conf"
SUP_CONF="${SUPERVISOR_DIR}/${DOMAIN}.conf"
if [[ ! -f "$SITE_CONF" && ! -f "$SUP_CONF" ]]; then
    echo "No port app for ${DOMAIN} (nothing to do)."
    exit 0
fi

# Back up so a failed reload can roll back (supervisor reread needs the files
# present to re-add them, so keep .bak copies until success).
cp -a "$NGINX_CONF" "${NGINX_CONF}.bak"
[[ -f "$SITE_CONF" ]] && cp -a "$SITE_CONF" "${SITE_CONF}.bak" || true
[[ -f "$SUP_CONF" ]] && cp -a "$SUP_CONF" "${SUP_CONF}.bak" || true

rm -f "$SITE_CONF" "$SUP_CONF"

if ! nginx_test_reload; then
    echo "Reload failed, restoring backup." >&2
    mv "${NGINX_CONF}.bak" "$NGINX_CONF"
    [[ -f "${SITE_CONF}.bak" ]] && mv "${SITE_CONF}.bak" "$SITE_CONF" || true
    [[ -f "${SUP_CONF}.bak" ]] && mv "${SUP_CONF}.bak" "$SUP_CONF" || true
    exit 1
fi
if ! supervisor_reread_update; then
    echo "Supervisor update failed, restoring backup." >&2
    mv "${NGINX_CONF}.bak" "$NGINX_CONF"
    [[ -f "${SITE_CONF}.bak" ]] && mv "${SITE_CONF}.bak" "$SITE_CONF" || true
    [[ -f "${SUP_CONF}.bak" ]] && mv "${SUP_CONF}.bak" "$SUP_CONF" || true
    nginx_test_reload || true
    supervisor_reread_update || true
    exit 1
fi
rm -f "${NGINX_CONF}.bak" "${SITE_CONF}.bak" "${SUP_CONF}.bak"
if [[ "$DELETE_DATA" -eq 1 && -d "${HOME_DIR}/data/${DOMAIN}" ]]; then
    rm -rf "${HOME_DIR}/data/${DOMAIN}"
    echo "Deleted data for ${DOMAIN}."
fi
echo "Removed port app ${DOMAIN}."
