#!/usr/bin/env bash
# Restore a dump into a site database. Run as root.
# Usage: restore-site-db.sh <db-name> <dump.sql.gz>
set -euo pipefail
[[ $# -eq 2 ]] || { echo "Usage: $0 <db-name> <dump.sql.gz>" >&2; exit 2; }
DB="$1"; DUMP="$2"
[[ "$DB" =~ ^[A-Za-z0-9_]+$ ]] || { echo "Bad DB name." >&2; exit 2; }
[[ -f "$DUMP" ]] || { echo "Dump not found." >&2; exit 2; }
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }
# Refuse garbage before touching the live DB.
gzip -t "$DUMP" || { echo "Dump is not valid gzip: $DUMP" >&2; exit 2; }
[[ -s "$DUMP" ]] || { echo "Dump is empty: $DUMP" >&2; exit 2; }
# Safety net: dump the current DB first (best-effort; a missing DB just warns).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"${SCRIPT_DIR}/backup-site-db.sh" "$DB" \
    || echo "WARNING: pre-restore backup of $DB failed; proceeding." >&2
# Import into a missing DB must not fail: create first (idempotent).
mariadb --protocol=socket -e "CREATE DATABASE IF NOT EXISTS \`${DB}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
gzip -dc "$DUMP" | mariadb --protocol=socket "$DB"
echo "Restored $DB from $DUMP"
