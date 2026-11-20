#!/usr/bin/env bash
# Install an SSH public key into a customer container's customer account.
# The container filesystem owns /home/customer, so keys are injected via
# podman exec (ephemeral - re-apply after image/container recreate).
# Usage: add-ssh-key.sh <service-user> <container-name> <pubkey-file>
set -euo pipefail

[[ $# -eq 3 ]] || { echo "Usage: $0 <service-user> <container-name> <pubkey-file>" >&2; exit 2; }
service_user="$1"; container_name="$2"; pubkey_file="$3"

[[ "$service_user" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || { echo "Invalid service user." >&2; exit 2; }
[[ "$container_name" =~ ^[a-zA-Z0-9_.-]+$ ]] || { echo "Invalid container name." >&2; exit 2; }
[[ -f "$pubkey_file" ]] || { echo "Key file not found." >&2; exit 2; }

if [[ "${EUID}" -ne 0 ]]; then echo "Run as root." >&2; exit 1; fi

key="$(cat "$pubkey_file")"
# Basic OpenSSH pubkey sanity check.
[[ "$key" =~ ^(ssh-ed25519|ecdsa-sha2-nistp[0-9]+|ssh-rsa|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com)\ [A-Za-z0-9+/=]+(\ .*)?$ ]] \
    || { echo "Not a valid OpenSSH public key." >&2; exit 2; }

uid="$(id -u "$service_user")"
home="$(getent passwd "$service_user" | cut -d: -f6)"
[[ -d "/run/user/${uid}" ]] || { echo "User manager not running (enable-linger first)." >&2; exit 1; }

runuser -u "$service_user" -- env HOME="$home" \
    XDG_RUNTIME_DIR="/run/user/${uid}" \
    DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
    podman exec -i "$container_name" /bin/bash -euo pipefail -s "$key" <<'REMOTE'
key="$1"
install -d -o customer -g customer -m 0700 /home/customer/.ssh
touch /home/customer/.ssh/authorized_keys
chown customer:customer /home/customer/.ssh/authorized_keys
chmod 0600 /home/customer/.ssh/authorized_keys
if ! grep -qxF "$key" /home/customer/.ssh/authorized_keys; then
    printf '%s\n' "$key" >> /home/customer/.ssh/authorized_keys
fi
REMOTE
echo "Installed key into ${container_name}:/home/customer/.ssh/authorized_keys"
