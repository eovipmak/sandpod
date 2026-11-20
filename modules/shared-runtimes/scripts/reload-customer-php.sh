#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
    echo "Run this helper as root; it switches to the customer's rootless account." >&2
    exit 1
fi

if [[ "$#" -ne 3 ]]; then
    echo "Usage: $0 <service-user> <container-name> <php-minor>" >&2
    exit 2
fi

service_user="$1"
container_name="$2"
php_minor="$3"

[[ "$service_user" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || {
    echo "Invalid service account name." >&2
    exit 2
}
[[ "$container_name" =~ ^[a-zA-Z0-9_.-]+$ ]] || {
    echo "Invalid container name." >&2
    exit 2
}
[[ "$php_minor" =~ ^8\.(2|3|4)$ ]] || {
    echo "Unsupported PHP minor version." >&2
    exit 2
}

uid="$(id -u "$service_user")"
home="$(getent passwd "$service_user" | cut -d: -f6)"
if [[ -z "$home" || ! -d "/run/user/${uid}" ]]; then
    echo "The service account needs an active lingering user manager." >&2
    exit 1
fi

runuser -u "$service_user" -- \
    env HOME="$home" \
        XDG_RUNTIME_DIR="/run/user/${uid}" \
        DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
    /bin/bash -c \
    'cd "$HOME" && podman exec "$1" /usr/local/bin/sandpod-runtime-reload "$2"' \
    -- "$container_name" "$php_minor"
