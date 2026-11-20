#!/usr/bin/env bash
# Back up one customer: ~/data + ~/config -> /var/backups/sandpod/<instance>-<ts>.tar.gz
# Run as root. Keeps last 7 daily archives per instance (configurable).
# Usage: backup-customer.sh <instance-id> [--service-user svc-c002] [--dir /var/backups/sandpod] [--keep 7]
set -euo pipefail
[[ $# -ge 1 ]] || { echo "Usage: $0 <instance-id> [--service-user ...] [--dir ...] [--keep 7]" >&2; exit 2; }
INSTANCE="$1"; shift
SERVICE_USER="svc-c${INSTANCE}"
DIR="/var/backups/sandpod"; KEEP=7
while [[ $# -gt 0 ]]; do case "$1" in
  --service-user) SERVICE_USER="${2:-}"; shift 2 ;;
  --dir) DIR="${2:-}"; shift 2 ;;
  --keep) KEEP="${2:-}"; shift 2 ;;
  *) echo "Unknown: $1" >&2; exit 2 ;;
esac; done
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
HOME_DIR="$(getent passwd "$SERVICE_USER" | cut -d: -f6)"
[[ -n "$HOME_DIR" && -d "$HOME_DIR/data" ]] || { echo "No data dir for $SERVICE_USER." >&2; exit 1; }
install -d -m 0700 "$DIR"
TS="$(date +%Y%m%d-%H%M%S)"
OUT="${DIR}/${INSTANCE}-${TS}.tar.gz"
tar -czpf "$OUT" -C "$HOME_DIR" data config
chmod 0600 "$OUT"
echo "$OUT"
# Prune old archives for this instance.
mapfile -t OLD < <(ls -1t "${DIR}/${INSTANCE}-"*.tar.gz 2>/dev/null || true)
if [[ ${#OLD[@]} -gt $KEEP ]]; then
    for f in "${OLD[@]:$KEEP}"; do rm -f "$f"; echo "Pruned $f"; done
fi
