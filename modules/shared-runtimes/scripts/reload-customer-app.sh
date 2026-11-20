#!/usr/bin/env bash
# Reload one customer port app (Node/Python) after a shared runtime update.
# Unlike PHP-FPM (USR2 to a long-lived master), port apps are plain
# supervisord programs, so a restart picks up the new /opt/sandpod binaries.
# Run as root.
#
# Usage: reload-customer-app.sh <service-user> <container> <domain>
set -euo pipefail

[[ $# -eq 3 ]] || { echo "Usage: $0 <service-user> <container> <domain>" >&2; exit 2; }
SERVICE_USER="$1"; CONTAINER="$2"; DOMAIN="${3,,}"

[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../../customer-apps/scripts/lib.sh
source "${SCRIPT_DIR}/../../customer-apps/scripts/lib.sh"

valid_domain "$DOMAIN" || { echo "Invalid domain: $DOMAIN" >&2; exit 2; }
PROG="$(program_for_domain "$DOMAIN")"
container_exec supervisorctl restart "$PROG"
container_exec supervisorctl status "$PROG"
