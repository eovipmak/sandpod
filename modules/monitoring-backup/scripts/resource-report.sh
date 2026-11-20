#!/usr/bin/env bash
# Report per-customer resource usage from the registry.
# Usage: resource-report.sh [--registry /etc/sandpod/customers.tsv]
set -euo pipefail
REGISTRY="/etc/sandpod/customers.tsv"
while [[ $# -gt 0 ]]; do case "$1" in --registry) REGISTRY="${2:-}"; shift 2;; *) echo "Unknown: $1" >&2; exit 2;; esac; done
printf '%-8s %-12s %-14s %s\n' INSTANCE USER CONTAINER STATS
grep -vP '^\s*(#|$)' "$REGISTRY" 2>/dev/null | while IFS=$'\t' read -r inst user cont _web _ssh _img; do
    uid="$(id -u "$user" 2>/dev/null || true)"
    [[ -z "$uid" ]] && { printf '%-8s %-12s %-14s %s\n' "$inst" "$user" "$cont" "no such user"; continue; }
    if [[ ! -d "/run/user/${uid}" ]]; then
        printf '%-8s %-12s %-14s %s\n' "$inst" "$user" "$cont" "user manager down"
        continue
    fi
    stats="$(runuser -u "$user" -- env HOME="$(getent passwd "$user" | cut -d: -f6)" \
        XDG_RUNTIME_DIR="/run/user/${uid}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
        podman stats --no-stream --format '{{.CPUPerc}} {{.MemUsage}}' "$cont" 2>/dev/null || echo "stopped stopped")"
    printf '%-8s %-12s %-14s %s\n' "$inst" "$user" "$cont" "$stats"
done
echo "Limits (template): 1 CPU / 1 GiB / 512 pids. Disk quota: host-storage (soft 8G / hard 10G per account)."
