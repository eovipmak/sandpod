#!/usr/bin/env bash
# Assign an XFS project to a customer home and cap it. Run as root, idempotent.
# One project per service account: projid = account UID, path = account home.
# Covers ~/data + ~/config AND rootless container storage (~/.local/share/containers).
# Usage: set-customer-quota.sh <service-user> [--hard-gb 10] [--soft-gb 8]
set -euo pipefail
[[ $# -ge 1 ]] || { echo "Usage: $0 <service-user> [--hard-gb 10] [--soft-gb 8]" >&2; exit 2; }
SERVICE_USER="$1"; shift
HARD_GB=10; SOFT_GB=8
while [[ $# -gt 0 ]]; do case "$1" in
  --hard-gb) HARD_GB="${2:-}"; shift 2 ;;
  --soft-gb) SOFT_GB="${2:-}"; shift 2 ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
id "$SERVICE_USER" >/dev/null 2>&1 || { echo "Unknown user." >&2; exit 1; }
[[ "$HARD_GB" =~ ^[0-9]+$ && "$SOFT_GB" =~ ^[0-9]+$ ]] || { echo "Quotas must be whole GB." >&2; exit 2; }
[[ "$SOFT_GB" -le "$HARD_GB" ]] || { echo "soft must be <= hard." >&2; exit 2; }

HOME_DIR="$(getent passwd "$SERVICE_USER" | cut -d: -f6)"
PROJID="$(id -u "$SERVICE_USER")"
[[ -d "$HOME_DIR" ]] || { echo "No home dir." >&2; exit 1; }

# /etc/projid + /etc/projects registry (xfs_quota name resolution).
touch /etc/projid /etc/projects
grep -qP "^${PROJID}:${SERVICE_USER}$" /etc/projid 2>/dev/null || {
    grep -vP "^(${PROJID}:|[^:]+:${SERVICE_USER}\$)" /etc/projid > /etc/projid.tmp || true
    printf '%s:%s\n' "$PROJID" "$SERVICE_USER" >> /etc/projid.tmp
    # keep file sorted, unique, without touching unrelated entries
    sort -u -t: -k1,1n /etc/projid.tmp -o /etc/projid
    rm -f /etc/projid.tmp
}
grep -qP "^${SERVICE_USER}:${HOME_DIR}\$" /etc/projects 2>/dev/null || {
    grep -vP "^${SERVICE_USER}:" /etc/projects > /etc/projects.tmp || true
    printf '%s:%s\n' "$SERVICE_USER" "$HOME_DIR" >> /etc/projects.tmp
    sort -u /etc/projects.tmp -o /etc/projects
    rm -f /etc/projects.tmp
}

# Tag the tree (works with or without accounting; persists in the fs).
xfs_quota -x -c "project -s -p ${HOME_DIR} ${PROJID}" / 2>&1 | grep -v 'skipping special file' || true
TAG="$(xfs_io -c 'stat' "$HOME_DIR" 2>/dev/null | sed -n 's/.*fsxattr.projid = \([0-9]*\).*/\1/p')"
if [[ "$TAG" != "$PROJID" ]]; then
    echo "WARNING: project tag missing on ${HOME_DIR} (got '${TAG:-?}'). Quota not applied." >&2; exit 1
fi

if ! grep -P '\s/\sxfs' /proc/mounts | grep -qE "pquota|prjquota"; then
    echo "PENDING: / has no pquota accounting (reboot first - see enable-project-quota.sh)."
    echo "Project ${SERVICE_USER} (id ${PROJID}) tagged at ${HOME_DIR}; limits apply on next run after reboot:"
    echo "  $0 ${SERVICE_USER} --hard-gb ${HARD_GB} --soft-gb ${SOFT_GB}"
    exit 3
fi

xfs_quota -x -c "limit -p bhard=${HARD_GB}g bsoft=${SOFT_GB}g ${PROJID}" / \
    || { echo "Setting limits failed." >&2; exit 1; }
echo "Quota ${SERVICE_USER}: soft=${SOFT_GB}G hard=${HARD_GB}G on ${HOME_DIR}"
xfs_quota -x -c "quota -N -p ${PROJID}" / 2>/dev/null || true
