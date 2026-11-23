#!/usr/bin/env bash
# Report per-customer resource usage from the registry.
# Read-only: never starts/stops anything, only podman stats + xfs_quota reads.
# Usage: resource-report.sh [--registry /etc/sandpod/customers.tsv]
#   [--xfs-report-file <file>] [--xfs-inode-report-file <file>] [--json]
#   The --xfs-*-report-file flags replay a saved `xfs_quota report` output
#   instead of calling xfs_quota (repo-test hooks; also usable offline).
# Exit: 0 ok, 1 runtime failure (missing registry), 2 bad arguments.
set -euo pipefail
REGISTRY="/etc/sandpod/customers.tsv"
XFS_REPORT_FILE=""
XFS_INODE_REPORT_FILE=""
JSON=0
while [[ $# -gt 0 ]]; do case "$1" in
  --registry) REGISTRY="${2:-}"; shift 2;;
  --xfs-report-file) XFS_REPORT_FILE="${2:-}"; shift 2;;
  --xfs-inode-report-file) XFS_INODE_REPORT_FILE="${2:-}"; shift 2;;
  --json) JSON=1; shift;;
  *) echo "Unknown: $1" >&2; exit 2;;
esac; done

[[ -f "$REGISTRY" ]] || { echo "Missing registry: $REGISTRY" >&2; exit 1; }

# Parse one `xfs_quota -x -c 'report -p -[b|i] -h'` file into assoc arrays.
# Real row layout:  "#<id> <name> <used> <soft> <hard> <warn> [grace...]"
# Header/footer lines never start with "#<digits>" or a bare project name
# followed by a size/count column, so they are skipped. Join key is exact:
# project NAME (== service user); numeric #id is a second exact key because
# projid == account UID (see host-storage).
declare -A R_USED=() R_SOFT=() R_HARD=()
load_xfs_report() { # <file> <blocks|inodes> (second arg only selects arrays)
    local src="$1" which="$2" line id name used soft hard
    [[ -n "$src" && -f "$src" ]] || return 0
    while IFS= read -r line; do
        [[ "$line" =~ ^[[:space:]]*#[0-9]+[[:space:]] ]] || continue
        # shellcheck disable=SC2086
        read -r id name used soft hard _rest <<<"$(echo "$line" | tr -s ' ' | sed 's/^ //')"
        [[ -n "${name:-}" && -n "${used:-}" ]] || continue
        [[ "$name" =~ ^[A-Za-z0-9_.-]+$ ]] || continue
        if [[ "$which" == "inodes" ]]; then
            I_USED["$name"]="$used"; I_SOFT["$name"]="${soft:-}"; I_HARD["$name"]="${hard:-}"
            I_BYID["${id#\#}"]="$name"
        else
            R_USED["$name"]="$used"; R_SOFT["$name"]="${soft:-}"; R_HARD["$name"]="${hard:-}"
            R_BYID["${id#\#}"]="$name"
        fi
    done < "$src"
}
declare -A I_USED=() I_SOFT=() I_HARD=()
declare -A R_BYID=() I_BYID=()
if [[ -n "$XFS_REPORT_FILE" ]]; then
    load_xfs_report "$XFS_REPORT_FILE" blocks
elif grep -P '\s/\sxfs' /proc/mounts 2>/dev/null | grep -qE "pquota|prjquota"; then
    tmp="$(mktemp /tmp/sandpod-xfs-report-XXXXXX)"
    trap 'rm -f "$tmp"' RETURN
    if xfs_quota -x -c 'report -p -b -h' / >"$tmp" 2>/dev/null; then
        load_xfs_report "$tmp" blocks
    fi
    rm -f "$tmp"
    trap - RETURN
fi
if [[ -n "$XFS_INODE_REPORT_FILE" ]]; then
    load_xfs_report "$XFS_INODE_REPORT_FILE" inodes
elif grep -P '\s/\sxfs' /proc/mounts 2>/dev/null | grep -qE "pquota|prjquota"; then
    tmp="$(mktemp /tmp/sandpod-xfs-inodes-XXXXXX)"
    trap 'rm -f "$tmp"' RETURN
    if xfs_quota -x -c 'report -p -i -h' / >"$tmp" 2>/dev/null; then
        load_xfs_report "$tmp" inodes
    fi
    rm -f "$tmp"
    trap - RETURN
fi

# Exact-match join: by project name, else by numeric project id (== uid).
disk_for_user() { # <user> <uid-or-empty>
    local user="$1" uid="${2:-}" name=""
    if [[ -n "${R_USED[$user]:-}" ]]; then
        name="$user"
    elif [[ -n "$uid" && -n "${R_BYID[$uid]:-}" ]]; then
        name="${R_BYID[$uid]}"
    else
        echo "quota-off"; return 0
    fi
    echo "${R_USED[$name]} used / ${R_SOFT[$name]} soft / ${R_HARD[$name]} hard"
}
inodes_for_user() { # <user> <uid-or-empty>
    local user="$1" uid="${2:-}" name=""
    if [[ -n "${I_USED[$user]:-}" ]]; then
        name="$user"
    elif [[ -n "$uid" && -n "${I_BYID[$uid]:-}" ]]; then
        name="${I_BYID[$uid]}"
    else
        echo "-"; return 0
    fi
    echo "${I_USED[$name]} used / ${I_SOFT[$name]} soft / ${I_HARD[$name]} hard"
}
json_escape() { # minimal: constrained charset (registry tsv), escape \ " + controls
    local s="$1"
    s="${s//\\/\\\\}"; s="${s//\"/\\\"}"
    s="${s//$'\n'/\\n}"; s="${s//$'\r'/\\r}"; s="${s//$'\t'/\\t}"
    printf '%s' "$s"
}

if [[ "$JSON" -eq 1 ]]; then
    first=1
    printf '['
    grep -vP '^\s*(#|$)' "$REGISTRY" 2>/dev/null | while IFS=$'\t' read -r inst user cont _web _ssh _img; do
        uid="$(id -u "$user" 2>/dev/null || true)"
        if [[ -z "$uid" ]]; then stats="no such user"
        elif [[ ! -d "/run/user/${uid}" ]]; then stats="user manager down"
        else stats="$(runuser -u "$user" -- env HOME="$(getent passwd "$user" | cut -d: -f6)" \
            XDG_RUNTIME_DIR="/run/user/${uid}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
            podman stats --no-stream --format '{{.CPUPerc}} {{.MemUsage}}' "$cont" 2>/dev/null || echo "stopped stopped")"
        fi
        [[ "$first" -eq 1 ]] && first=0 || printf ','
        printf '{"instance":"%s","user":"%s","container":"%s","stats":"%s","disk":"%s","inodes":"%s"}' \
            "$(json_escape "$inst")" "$(json_escape "$user")" "$(json_escape "$cont")" \
            "$(json_escape "$stats")" "$(json_escape "$(disk_for_user "$user" "$uid")")" \
            "$(json_escape "$(inodes_for_user "$user" "$uid")")"
    done
    printf ']\n'
else
    printf '%-8s %-12s %-14s %-28s %-32s %s\n' INSTANCE USER CONTAINER STATS DISK INODES
    grep -vP '^\s*(#|$)' "$REGISTRY" 2>/dev/null | while IFS=$'\t' read -r inst user cont _web _ssh _img; do
        uid="$(id -u "$user" 2>/dev/null || true)"
        if [[ -z "$uid" ]]; then
            printf '%-8s %-12s %-14s %-28s %-32s %s\n' "$inst" "$user" "$cont" "no such user" "$(disk_for_user "$user" "")" "$(inodes_for_user "$user" "")"
            continue
        fi
        if [[ ! -d "/run/user/${uid}" ]]; then
            printf '%-8s %-12s %-14s %-28s %-32s %s\n' "$inst" "$user" "$cont" "user manager down" "$(disk_for_user "$user" "$uid")" "$(inodes_for_user "$user" "$uid")"
            continue
        fi
        stats="$(runuser -u "$user" -- env HOME="$(getent passwd "$user" | cut -d: -f6)" \
            XDG_RUNTIME_DIR="/run/user/${uid}" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${uid}/bus" \
            podman stats --no-stream --format '{{.CPUPerc}} {{.MemUsage}}' "$cont" 2>/dev/null || echo "stopped stopped")"
        printf '%-8s %-12s %-14s %-28s %-32s %s\n' "$inst" "$user" "$cont" "$stats" "$(disk_for_user "$user" "$uid")" "$(inodes_for_user "$user" "$uid")"
    done
    echo "Limits (template): 1 CPU (quota 100%) / 1 GiB RAM, swap 0 / 512 pids (TasksMax 1024) / IOWeight 100. Disk quota: host-storage (soft 8G / hard 10G per account)."
fi
