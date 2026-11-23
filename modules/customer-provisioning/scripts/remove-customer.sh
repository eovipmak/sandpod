#!/usr/bin/env bash
# Remove a customer service (stop unit + Quadlet files). Data is kept by default.
# Usage: remove-customer.sh <instance-id> [--service-user svc-c002]
#   [--delete-data] [--delete-user] [--delete-registry-entry]
#   [--registry /etc/sandpod/customers.tsv]
set -euo pipefail

[[ $# -ge 1 ]] || { echo "Usage: $0 <instance-id> [--service-user ...] [--delete-data] [--delete-user]" >&2; exit 2; }
INSTANCE="$1"; shift
[[ "$INSTANCE" =~ ^[0-9]{3}$ ]] || { echo "instance-id must be 3 digits." >&2; exit 2; }

SERVICE_USER="svc-c${INSTANCE}"
DELETE_DATA=0; DELETE_USER=0; DELETE_REG=0
REGISTRY="/etc/sandpod/customers.tsv"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --service-user) SERVICE_USER="${2:-}"; shift 2 ;;
        --delete-data) DELETE_DATA=1; shift ;;
        --delete-user) DELETE_USER=1; shift ;;
        --delete-registry-entry) DELETE_REG=1; shift ;;
        --registry) REGISTRY="${2:-}"; shift 2 ;;
        *) echo "Unknown arg: $1" >&2; exit 2 ;;
    esac
done

[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
id "$SERVICE_USER" >/dev/null 2>&1 || { echo "User ${SERVICE_USER} not found." >&2; exit 1; }
HOME_DIR="$(getent passwd "$SERVICE_USER" | cut -d: -f6)"
UID_NUM="$(id -u "$SERVICE_USER")"

user_systemctl() {
    runuser -u "$SERVICE_USER" -- env HOME="$HOME_DIR" \
        XDG_RUNTIME_DIR="/run/user/${UID_NUM}" \
        DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${UID_NUM}/bus" \
        systemctl --user "$@" || true
}

echo "Stopping customer@${INSTANCE}.service ..."
user_systemctl stop "customer@${INSTANCE}.service"
user_systemctl disable "customer@${INSTANCE}.service"

UNIT_DIR="${HOME_DIR}/.config/containers/systemd"
rm -f "${UNIT_DIR}/customer@${INSTANCE}.container"
rm -rf "${UNIT_DIR}/customer@${INSTANCE}.container.d"
# Remove shared template only if no other instances remain.
if ! compgen -G "${UNIT_DIR}/customer@*.container" >/dev/null; then
    rm -f "${UNIT_DIR}/customer@.container"
fi
user_systemctl daemon-reload

if [[ "$DELETE_DATA" -eq 1 ]]; then
    echo "Deleting ${HOME_DIR}/data and ${HOME_DIR}/config ..."
    rm -rf "${HOME_DIR}/data" "${HOME_DIR}/config"
fi
if [[ "$DELETE_REG" -eq 1 && -f "$REGISTRY" ]]; then
    (
        flock -x 9
        grep -v -P "^${INSTANCE}\t" "$REGISTRY" > "${REGISTRY}.tmp" || true
        mv "${REGISTRY}.tmp" "$REGISTRY"
    ) 9>"${REGISTRY}.lock"
fi
if [[ "$DELETE_USER" -eq 1 ]]; then
    [[ "$DELETE_DATA" -eq 1 ]] || { echo "Refusing --delete-user without --delete-data." >&2; exit 2; }
    loginctl disable-linger "$SERVICE_USER" || true
    userdel -r "$SERVICE_USER" || userdel "$SERVICE_USER"
    echo "Deleted user ${SERVICE_USER}"
fi
echo "Removed instance ${INSTANCE} (data kept: $([[ $DELETE_DATA -eq 0 ]] && echo yes || echo no))."
