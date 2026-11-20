#!/usr/bin/env bash
# List sites configured in a customer's nginx maps + port apps. Read-only.
# Usage: list-sites.sh <service-user> <container> [--json]
#   Default prints human-readable text (stable). --json prints one JSON object
#   for the control panel (see docs/control-panel.md, Spec v2).
# Exit codes: 0 ok, 1 environment problem, 2 bad arguments.
set -euo pipefail

JSON=0
ARGS=()
for a in "$@"; do
    case "$a" in
        --json) JSON=1 ;;
        *) ARGS+=("$a") ;;
    esac
done
[[ ${#ARGS[@]} -eq 2 ]] || { echo "Usage: $0 <service-user> <container> [--json]" >&2; exit 2; }
SERVICE_USER="${ARGS[0]}"; CONTAINER="${ARGS[1]}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"

if [[ "$JSON" -eq 1 ]]; then
    python3 - "$NGINX_CONF" "$SITES_DIR" "$SUPERVISOR_DIR" <<'PYEOF'
import glob, json, os, re, sys
nginx_conf, sites_dir, sup_dir = sys.argv[1:4]
src = open(nginx_conf).read()

def parse_map(var):
    m = re.search(r'map\s+\$host\s+\$%s\s*\{(.*?)\}' % re.escape(var), src, re.S)
    out = {}
    if m:
        for line in m.group(1).strip().splitlines():
            line = line.strip()
            if not line or line.startswith('#'):
                continue
            parts = line.rstrip(';').split()
            if len(parts) >= 2:
                out[parts[0]] = ' '.join(parts[1:])
    return out

roots = parse_map('site_root')
backends = parse_map('php_fpm_backend')
domains = sorted(set(roots) | set(backends) - {'default'})
php_sites = [
    {'domain': d, 'root': roots.get(d), 'backend': backends.get(d)}
    for d in domains
]

port_apps = []
for conf in sorted(glob.glob(os.path.join(sites_dir, '*.conf'))):
    domain = os.path.splitext(os.path.basename(conf))[0]
    csrc = open(conf).read()
    m = re.search(r'proxy_pass\s+(http://127\.0\.0\.1:(\d+))', csrc)
    target = m.group(1) if m else None
    port = int(m.group(2)) if m else None
    sup = os.path.join(sup_dir, domain + '.conf')
    prog, cmd, directory = None, None, None
    if os.path.isfile(sup):
        ssrc = open(sup).read()
        sm = re.search(r'(?m)^\[program:(.+?)\]', ssrc)
        cm = re.search(r'(?m)^command\s*=\s*(.+)$', ssrc)
        dm = re.search(r'(?m)^directory\s*=\s*(.+)$', ssrc)
        prog = sm.group(1).strip() if sm else None
        cmd = cm.group(1).strip() if cm else None
        directory = dm.group(1).strip() if dm else None
    port_apps.append({
        'domain': domain, 'port': port, 'target': target,
        'program': prog, 'command': cmd, 'directory': directory,
    })

print(json.dumps({
    'defaults': {
        'site_root': roots.get('default'),
        'php_fpm_backend': backends.get('default'),
    },
    'php_sites': php_sites,
    'port_apps': port_apps,
}, indent=2, sort_keys=False))
PYEOF
    exit 0
fi

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
