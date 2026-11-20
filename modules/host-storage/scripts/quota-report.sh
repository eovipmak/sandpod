#!/usr/bin/env bash
# Per-customer disk usage vs project-quota limits. Run as root. Read-only.
# Usage: quota-report.sh [--registry /etc/sandpod/customers.tsv]
set -euo pipefail
REGISTRY="/etc/sandpod/customers.tsv"
while [[ $# -gt 0 ]]; do case "$1" in --registry) REGISTRY="${2:-}"; shift 2;; *) echo "Unknown: $1" >&2; exit 2;; esac; done

if ! grep -P '\s/\sxfs' /proc/mounts | grep -qE "pquota|prjquota"; then
    echo "Project quota accounting is OFF (see host-storage enable-project-quota.sh + reboot)."
    echo "Falling back to du (slow, no enforcement):"
    grep -vP '^\s*(#|$)' "$REGISTRY" 2>/dev/null | while IFS=$'\t' read -r inst user _c _w _s _i; do
        home="$(getent passwd "$user" 2>/dev/null | cut -d: -f6 || true)"
        if [[ -n "$home" && -d "$home" ]]; then
            echo -n "$user($inst): "; du -sh "$home" 2>/dev/null | cut -f1
        else
            echo "$user($inst): no home"
        fi
    done
    exit 0
fi

IDS="$(awk -F: '{print "#"$1}' /etc/projid 2>/dev/null | paste -sd'|' - || true)"
xfs_quota -x -c 'report -p -h' / 2>/dev/null | awk -v ids="^(${IDS})$" '
  /^Project quota/ {print; getline; print; next}
  $1 ~ ids {print}
'
