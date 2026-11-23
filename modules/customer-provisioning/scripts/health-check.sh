#!/usr/bin/env bash
# Health-check a customer web port. Expects the pilot nginx map fixtures.
# Usage: health-check.sh <web-port> [base-host]
# Exit 0 when all routes return HTTP 200, else 1.
set -euo pipefail

PORT="${1:-}"
BASE="${2:-127.0.0.1}"
[[ "$PORT" =~ ^[0-9]+$ && "$PORT" -ge 1 && "$PORT" -le 65535 ]] \
    || { echo "Usage: $0 <web-port 1-65535> [base-host]" >&2; exit 2; }
command -v curl >/dev/null 2>&1 || { echo "Missing curl." >&2; exit 1; }

fail=0
BODY="$(mktemp /tmp/sandpod-hc-body-XXXXXX)"
trap 'rm -f "$BODY"' EXIT
check() {
    local host="$1" path="$2" expect_version="$3"
    local code body
    code="$(curl -s --max-time 10 -o "$BODY" -w '%{http_code}' -H "Host: ${host}" "http://${BASE}:${PORT}${path}" || echo 000)"
    body="$(cat "$BODY" 2>/dev/null || true)"
    if [[ "$code" == "200" ]]; then
        if [[ -n "$expect_version" && "$body" != *"$expect_version"* ]]; then
            echo "FAIL ${host}${path}: 200 but body lacks '${expect_version}' (got: ${body:0:80})"
            fail=1
        else
            echo "OK   ${host}${path}: ${code} ${body:0:60}"
        fi
    else
        echo "FAIL ${host}${path}: HTTP ${code}"
        fail=1
    fi
}

check "php82.test" "/index.php" "8.2"
check "php83.test" "/index.php" "8.3"
check "php84.test" "/index.php" "8.4"
check "php85.test" "/index.php" "8.5"
check "php86.test" "/index.php" "8.6"
check "localhost" "/" ""
exit "$fail"
