#!/usr/bin/env bash
# Dump one site database to /var/backups/sandpod/<db>-<ts>.sql.gz. Run as root.
# Usage: backup-site-db.sh <db-name> [--dir /var/backups/sandpod]
set -euo pipefail
[[ $# -ge 1 ]] || { echo "Usage: $0 <db-name> [--dir /var/backups/sandpod]" >&2; exit 2; }
DB="$1"; shift
DIR="/var/backups/sandpod"
while [[ $# -gt 0 ]]; do case "$1" in --dir) DIR="${2:-}"; shift 2;; *) echo "Unknown: $1" >&2; exit 2;; esac; done
[[ "$DB" =~ ^[A-Za-z0-9_]+$ ]] || { echo "Bad DB name." >&2; exit 2; }
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
install -d -m 0700 "$DIR"
TS="$(date +%Y%m%d-%H%M%S)"
OUT="${DIR}/${DB}-${TS}.sql.gz"
mariadb-dump --protocol=socket --single-transaction --routines --triggers "$DB" | gzip -c > "$OUT"
chmod 0600 "$OUT"
echo "$OUT"
