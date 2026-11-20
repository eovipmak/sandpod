#!/usr/bin/env bash
# List sites configured in a customer's nginx maps + port apps. Read-only.
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
echo 'port_apps:'
python3 - "$SITES_DIR" "$SUPERVISOR_DIR" <<'PYEOF'
import glob, os, re, sys
sites_dir, sup_dir = sys.argv[1:3]
found = False
for conf in sorted(glob.glob(os.path.join(sites_dir, '*.conf'))):
    domain = os.path.splitext(os.path.basename(conf))[0]
    src = open(conf).read()
    m = re.search(r'proxy_pass\s+(http://127\.0\.0\.1:\d+)', src)
    port = m.group(1) if m else '(proxy target unknown)'
    sup = os.path.join(sup_dir, domain + '.conf')
    prog = '(no supervisor conf)'
    if os.path.isfile(sup):
        sm = re.search(r'(?m)^\[program:(.+?)\]', open(sup).read())
        if sm:
            prog = sm.group(1)
    print('  %s -> %s [%s]' % (domain, port, prog))
    found = True
if not found:
    print('  (none)')
PYEOF
