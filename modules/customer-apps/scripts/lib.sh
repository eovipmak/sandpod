# Shared bits for customer-apps scripts. Source it, don't run it.
# Expects: SERVICE_USER, CONTAINER set by caller. Provides:
#   HOME_DIR UID_NUM  - account home + uid (fails if user manager is down)
#   container_exec    - podman exec as the service account (proper XDG/DBUS env)
#   nginx_test_reload - nginx -t + reload inside the container
#   valid_domain / valid_minor / backend_for_minor
APP_LIB_VERSION=1

[[ -n "${SERVICE_USER:-}" && -n "${CONTAINER:-}" ]] || {
    echo "lib.sh: SERVICE_USER and CONTAINER must be set." >&2; return 2 2>/dev/null || exit 2
}

[[ "$SERVICE_USER" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || { echo "Invalid service user." >&2; exit 2; }
[[ "$CONTAINER" =~ ^[a-zA-Z0-9_.-]+$ ]] || { echo "Invalid container name." >&2; exit 2; }
id "$SERVICE_USER" >/dev/null 2>&1 || { echo "Unknown user ${SERVICE_USER}." >&2; exit 1; }

HOME_DIR="$(getent passwd "$SERVICE_USER" | cut -d: -f6)"
UID_NUM="$(id -u "$SERVICE_USER")"
[[ -n "$HOME_DIR" && -d "/run/user/${UID_NUM}" ]] || {
    echo "User manager for ${SERVICE_USER} is down (enable-linger first)." >&2; exit 1
}
NGINX_CONF="${HOME_DIR}/config/nginx.conf"
[[ -f "$NGINX_CONF" ]] || { echo "Missing ${NGINX_CONF}." >&2; exit 1; }

container_exec() {
    runuser -u "$SERVICE_USER" -- env HOME="$HOME_DIR" \
        XDG_RUNTIME_DIR="/run/user/${UID_NUM}" \
        DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${UID_NUM}/bus" \
        podman exec "$CONTAINER" "$@"
}

nginx_test_reload() {
    container_exec nginx -t || return 1
    container_exec nginx -s reload || return 1
    # HUP returns after signalling; give old workers a moment to drain so
    # the next request hits the new config.
    sleep 1
}

valid_domain() {
    [[ "${1,,}" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$ ]]
}

valid_minor() { [[ "$1" =~ ^8\.[2-5]$ ]]; }

backend_for_minor() {
    case "$1" in
        8.2) echo "127.0.0.1:9082" ;;
        8.3) echo "127.0.0.1:9000" ;;
        8.4) echo "127.0.0.1:9084" ;;
        8.5) echo "127.0.0.1:9085" ;;
        *) echo "Unsupported minor: $1" >&2; return 2 ;;
    esac
}

# Set/replace one entry in a named nginx map inside $NGINX_CONF (idempotent).
# Usage: map_set <map-variable> <key> <value>   (key "default" updates the default)
map_set() {
    local var="$1" key="$2" value="$3"
    python3 - "$NGINX_CONF" "$var" "$key" "$value" <<'PYEOF'
import re, sys
path, var, key, value = sys.argv[1:5]
src = open(path).read()
m = re.search(r'(map\s+\$host\s+\$%s\s*\{(.*?)\})' % re.escape(var), src, re.S)
if not m:
    sys.exit('map $%s not found in %s' % (var, path))
block = m.group(2)
pat = r'(?m)^([ \t]+)' + re.escape(key) + r'[ \t]+[^;\n]+;\s*\n'
if re.search(pat, block):
    block = re.sub(pat, r'\g<1>' + key + '  ' + value + ';\n', block, count=1)
else:
    anchor = re.search(r'(?m)^([ \t]+)default[ \t]+[^;\n]+;\s*\n', block)
    if key == 'default':
        block = re.sub(r'(?m)^([ \t]+)default[ \t]+[^;\n]+;\s*\n',
                       r'\g<1>default  ' + value + ';\n', block, count=1)
    elif anchor:
        block = block.replace(anchor.group(0), anchor.group(0) + anchor.group(1) + key + '  ' + value + ';\n', 1)
    else:
        block = block + '        ' + key + '  ' + value + ';\n'
src = src[:m.start(2)] + block + src[m.end(2):]
open(path, 'w').write(src)
PYEOF
}

# Delete one non-default entry from a named map (no-op if absent).
# Usage: map_del <map-variable> <key>
map_del() {
    local var="$1" key="$2"
    python3 - "$NGINX_CONF" "$var" "$key" <<'PYEOF'
import re, sys
path, var, key = sys.argv[1:4]
assert key != 'default', 'refusing to delete the default entry'
src = open(path).read()
m = re.search(r'(map\s+\$host\s+\$%s\s*\{(.*?)\})' % re.escape(var), src, re.S)
if not m:
    sys.exit('map $%s not found in %s' % (var, path))
block = re.sub(r'(?m)^[ \t]+' + re.escape(key) + r'[ \t]+[^;\n]+;\s*\n', '', m.group(2), count=1)
open(path, 'w').write(src[:m.start(2)] + block + src[m.end(2):])
PYEOF
}
