#!/usr/bin/env bash
# Create a per-site database + user on the host MariaDB. Run as root.
# The container reaches the host via host.containers.internal (NOT 127.0.0.1,
# which is the container's own netns). MariaDB must bind that address - see README.
# Usage: create-site-db.sh <db-name> <db-user> [--host host.containers.internal]
# Prints a credential block once; the control panel stores it (this script never writes it to disk).
set -euo pipefail

[[ $# -ge 2 ]] || { echo "Usage: $0 <db-name> <db-user> [--host host.containers.internal]" >&2; exit 2; }
DB="$1"; USER="$2"; shift 2
DB_HOST="host.containers.internal"
while [[ $# -gt 0 ]]; do case "$1" in
  --host) DB_HOST="${2:-}"; shift 2 ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done

[[ "$DB" =~ ^[A-Za-z0-9_]+$ ]] || { echo "DB name must be [A-Za-z0-9_]." >&2; exit 2; }
[[ "$USER" =~ ^[A-Za-z0-9_]+$ ]] || { echo "DB user must be [A-Za-z0-9_]." >&2; exit 2; }
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

PASS="$(openssl rand -base64 24 | tr -d '\n')"
# Root auth: AlmaLinux MariaDB defaults to unix_socket for root, so no password flag.
mariadb --protocol=socket <<SQL
CREATE DATABASE IF NOT EXISTS \`${DB}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS '${USER}'@'%' IDENTIFIED BY '${PASS}';
ALTER USER '${USER}'@'%' IDENTIFIED BY '${PASS}';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, INDEX, ALTER,
      CREATE TEMPORARY TABLES, LOCK TABLES, EXECUTE, REFERENCES
  ON \`${DB}\`.* TO '${USER}'@'%';
FLUSH PRIVILEGES;
SQL

cat <<CREDS
# Store these in the control panel vault - they are not saved on the host.
DB_HOST=${DB_HOST}
DB_NAME=${DB}
DB_USER=${USER}
DB_PASS=${PASS}
# Container test (after granting host firewall/bind, see README):
#   mysql -h \$DB_HOST -u \$DB_USER -p \$DB_NAME -e 'SELECT 1;'
CREDS
