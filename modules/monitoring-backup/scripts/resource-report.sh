#!/usr/bin/env bash
# Report per-customer resource usage from the registry.
# Usage: resource-report.sh [--registry /etc/sandpod/customers.tsv]
#   [--xfs-report-file <file>]  (repo-test hook: use a saved `xfs_quota report -p -b -h` output instead of calling xfs_quota)
set -euo pipefail
REGISTRY="/etc/sandpod/customers.tsv"
XFS_REPORT_FILE=""
while [[ $# -gt 0 ]]; do case "$1" in
  --registry) REGISTRY="${2:-}"; shift 2;;
  --xfs-report-file) XFS_REPORT_FILE="${2:-}"; shift 2;;
  *) echo "Unknown: $1" >&2; exit 2;;
esac; done

# Build USER -> DISK lookup from XFS project report (best-effort, read-only).
# Expected `xfs_quota -x -c 'report -p -h'` lines contain the project name
# (== service user) plus Used/Soft/Hard columns. Fixture files for tests use
# the same layout.
declare -A DISK_OF=()
load_xfs_report() {
    local src="$1"
    [[ -n "$src" && -f "$src" ]] || return 0
    while IFS= read -r line; do
        # Skip headers/blank lines; match lines starting with "#<id>" or project name.
        [[ "$line" =~ ^[[:space:]]*(#[0-9]+|[A-Za-z0-9_.-]+)[[:space:]] ]] || continue
        [[ "$line" =~ Project[[:space:]]+quota ]] && continue
        # Heuristic columns: $1=id-or-name ... find a token that equals a known user later;
        # here just record "<first-token> <rest>" keyed by any token matching a service user.
        # The join happens below by grepping the line for " $user ".
        XFS_LINES+=("$line")
    done < "$src"
}
XFS_LINES=()
if [[ -n "$XFS_REPORT_FILE" ]]; then
    load_xfs_report "$XFS_REPORT_FILE"
elif grep -P '\s/\sxfs' /proc/mounts 2>/dev/null | grep -qE "pquota|prjquota"; then
    tmp="$(mktemp /tmp/sandpod-xfs-report-XXXXXX)"
    trap 'rm -f "$tmp"' RETURN
    if xfs_quota -x -c 'report -p -b -h' / >"$tmp" 2>/dev/null; then
        load_xfs_report "$tmp"
    fi
    rm -f "$tmp"
    trap - RETURN
fi
disk_for_user() {
    local user="$1" l
    for l in ${XFS_LINES[@]+"${XFS_LINES[@]}"}; do
        # Match "#<id> <user>" or standalone "<user>" column.
        if [[ " $l " == *" $user "* ]]; then
            # Squeeze spaces, print Used/Soft/Hard-ish tail (cols 2-4 after name when possible).
            echo "$l" | tr -s ' ' | sed 's/^ //'
            return 0
        fi
    done
    echo "quota-off"
}

printf '%-8s %-12s %-14s %-28s %s\n' INSTANCE USER CONTAINER STATS DISK
grep -vP '^\s*(#|$)' "$REGISTRY" 2>/dev/null | while IFS=$'\t' read -r inst user cont _web _ssh _img; do
    uid="$(id -u "$user" 2>/dev/null || true)"
    if [[ -z "$uid" ]]; then
        printf '%-8s %-12s %-14s %-28s %s\n' "$inst" "$user" "$cont" "no such user" "$(disk_for_user "$user")"
        continue
    fi
    if [[ ! -d "/run/user/${uid}" ]]; then
        printf '%-8s %-12s %-14s %-28s %s\n' "$inst" "$user" "$cont" "user manager down" "$(disk_for_user "$user")"
        continue
    fi
    stats="$(runuser -u "$user" -- env HOME="$(getent passwd "$user" | cut -d: -f6)" \
        XDG_RUNTIME_DIR="/run/user/${uid}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
        podman stats --no-stream --format '{{.CPUPerc}} {{.MemUsage}}' "$cont" 2>/dev/null || echo "stopped stopped")"
    printf '%-8s %-12s %-14s %-28s %s\n' "$inst" "$user" "$cont" "$stats" "$(disk_for_user "$user")"
done
echo "Limits (template): 1 CPU (quota 100%) / 1 GiB RAM, swap 0 / 512 pids (TasksMax 1024) / IOWeight 100. Disk quota: host-storage (soft 8G / hard 10G per account)."
