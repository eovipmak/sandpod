#!/usr/bin/env bash
# Restore a dump into a site database. Run as root.
# Usage: restore-site-db.sh <db-name> <dump.sql.gz>
set -euo pipefail
[[ $# -eq 2 ]] || { echo "Usage: $0 <db-name> <dump.sql.gz>" >&2; exit 2; }
DB="$1"; DUMP="$2"
[[ "$DB" =~ ^[A-Za-z0-9_]+$ ]] || { echo "Bad DB name." >&2; exit 2; }
[[ -f "$DUMP" ]] || { echo "Dump not found." >&2; exit 2; }
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
gzip -dc "$DUMP" | mariadb --protocol=socket "$DB"
echo "Restored $DB from $DUMP"
