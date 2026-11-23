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
[[ "$DIR" == /* ]] || { echo "--dir must be absolute." >&2; exit 2; }
install -d -m 0700 "$DIR"
if command -v getenforce >/dev/null 2>&1 && [[ "$(getenforce)" == "Enforcing" ]]; then
    restorecon -RF "$DIR" 2>/dev/null || true
fi
TS="$(date +%Y%m%d-%H%M%S)"
OUT="${DIR}/${DB}-${TS}.sql.gz"
# Write to .tmp first: a failed/empty dump must never look like a backup.
if ! mariadb-dump --protocol=socket --single-transaction --routines --triggers "$DB" | gzip -c > "${OUT}.tmp"; then
    rm -f "${OUT}.tmp"; echo "Dump of $DB failed." >&2; exit 1
fi
[[ -s "${OUT}.tmp" ]] || { rm -f "${OUT}.tmp"; echo "Dump of $DB is empty." >&2; exit 1; }
gzip -t "${OUT}.tmp" || { rm -f "${OUT}.tmp"; echo "Dump of $DB failed integrity check." >&2; exit 1; }
mv "${OUT}.tmp" "$OUT"
chmod 0600 "$OUT"
echo "$OUT"
