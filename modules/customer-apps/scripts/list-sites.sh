#!/usr/bin/env bash
# List sites configured in a customer's nginx maps. Read-only.
# Usage: list-sites.sh <service-user> <container>
set -euo pipefail
[[ $# -eq 2 ]] || { echo "Usage: $0 <service-user> <container>" >&2; exit 2; }
SERVICE_USER="$1"; CONTAINER="$2"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"
python3 - "$NGINX_CONF" <<'PYEOF'
import re, sys
src = open(sys.argv[1]).read()
for var in ('site_root', 'php_fpm_backend'):
    m = re.search(r'map\s+\$host\s+\$%s\s*\{(.*?)\}' % var, src, re.S)
    print('$%s:' % var)
    if not m:
        print('  (map missing)'); continue
    for line in m.group(1).strip().splitlines():
        line = line.strip()
        if line and not line.startswith('#'):
            print('  ' + line)
PYEOF
