#!/usr/bin/env bash
# Fleet Node/Python patch rollout: update host runtime trees, then restart
# each customer's port apps in batches with health-checks.
# (Mirrors rollout-php-patch.sh, but port apps are supervisord programs, not
# FPM masters: a restart picks up the new /opt/sandpod binaries. There is no
# per-minor USR2 - every port app restarts regardless of runtime.)
# Run as root. Stops on first batch failure (fix forward, then re-run).
#
# Usage: rollout-app-patch.sh [--batch-size 5] [--sleep-seconds 10]
#   [--skip-node-update] [--skip-python-update] [--skip-update]
#   [--registry /etc/sandpod/customers.tsv]
#   [--node-args "..."] [--python-args "..."] (extra args forwarded to updaters)
set -euo pipefail

BATCH=5; SLEEP_S=10; SKIP_NODE=0; SKIP_PY=0
REGISTRY="/etc/sandpod/customers.tsv"
NODE_ARGS=(); PY_ARGS=()
while [[ $# -gt 0 ]]; do case "$1" in
  --batch-size) BATCH="${2:-}"; shift 2 ;;
  --sleep-seconds) SLEEP_S="${2:-}"; shift 2 ;;
  --skip-node-update) SKIP_NODE=1; shift ;;
  --skip-python-update) SKIP_PY=1; shift ;;
  --skip-update) SKIP_NODE=1; SKIP_PY=1; shift ;;
  --registry) REGISTRY="${2:-}"; shift 2 ;;
  --node-args) NODE_ARGS+=("${2:-}"); shift 2 ;;
  --python-args) PY_ARGS+=("${2:-}"); shift 2 ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done
[[ "$BATCH" =~ ^[0-9]+$ && "$BATCH" -ge 1 ]] || { echo "--batch-size must be a positive integer." >&2; exit 2; }
[[ "$SLEEP_S" =~ ^[0-9]+$ ]] || { echo "--sleep-seconds must be a non-negative integer." >&2; exit 2; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
RELOAD="${REPO_ROOT}/modules/shared-runtimes/scripts/reload-customer-app.sh"
LISTSITES="${REPO_ROOT}/modules/customer-apps/scripts/list-sites.sh"
HEALTH="${REPO_ROOT}/modules/customer-provisioning/scripts/health-check.sh"

[[ -f "$REGISTRY" ]] || { echo "Missing $REGISTRY." >&2; exit 1; }

if [[ "$SKIP_NODE" -eq 0 ]]; then
    echo "== Updating host Node.js trees =="
    if ((${#NODE_ARGS[@]})); then
        "${REPO_ROOT}/modules/shared-runtimes/scripts/update-node-runtimes.sh" "${NODE_ARGS[@]}"
    else
        "${REPO_ROOT}/modules/shared-runtimes/scripts/update-node-runtimes.sh"
    fi
else
    echo "== Skipping host Node.js update =="
fi
if [[ "$SKIP_PY" -eq 0 ]]; then
    echo "== Updating host Python trees =="
    if ((${#PY_ARGS[@]})); then
        "${REPO_ROOT}/modules/shared-runtimes/scripts/update-python-runtimes.sh" "${PY_ARGS[@]}"
    else
        "${REPO_ROOT}/modules/shared-runtimes/scripts/update-python-runtimes.sh"
    fi
else
    echo "== Skipping host Python update =="
fi

mapfile -t CUSTOMERS < <(grep -vP '^\s*(#|$)' "$REGISTRY")
[[ ${#CUSTOMERS[@]} -gt 0 ]] || { echo "Registry empty." >&2; exit 1; }
echo "== Rolling out to ${#CUSTOMERS[@]} customers, batch-size ${BATCH} =="

port_app_domains() {
    # Prints one domain per line for the customer's port apps (empty = none).
    local user="$1" cont="$2"
    "$LISTSITES" "$user" "$cont" --json | python3 -c \
        'import json,sys; print("\n".join(a["domain"] for a in json.load(sys.stdin).get("port_apps", [])))'
}

fail=0; batch_n=0; batch_members=()
flush_batch() {
    [[ ${#batch_members[@]} -eq 0 ]] && return 0
    batch_n=$((batch_n+1))
    echo "--- Batch ${batch_n}: ${batch_members[*]} ---"
    for entry in "${batch_members[@]}"; do
        IFS=$'\t' read -r inst user cont web _ssh _img <<<"$entry"
        echo "Port apps for ${cont} ..."
        domains="$(port_app_domains "$user" "$cont")" || { echo "FAIL list-sites ${cont}" >&2; fail=1; continue; }
        if [[ -z "${domains//[[:space:]]/}" ]]; then
            echo "(no port apps for ${cont}, skipping restarts)"
        else
            while IFS= read -r domain; do
                [[ -n "$domain" ]] || continue
                echo "Restart ${cont} ${domain} ..."
                if ! "$RELOAD" "$user" "$cont" "$domain"; then
                    echo "FAIL restart ${cont} ${domain}" >&2; fail=1
                fi
            done <<<"$domains"
        fi
        echo "Health-check ${cont} (port ${web}) ..."
        if ! "$HEALTH" "$web"; then
            echo "FAIL health ${cont}" >&2; fail=1
        fi
    done
    batch_members=()
    if [[ "$fail" -ne 0 ]]; then
        echo "Batch ${batch_n} FAILED - stopping (fix forward, re-run with --skip-update)." >&2
        return 1
    fi
    echo "Batch ${batch_n} OK. Sleeping ${SLEEP_S}s ..."
    sleep "$SLEEP_S"
}

for entry in "${CUSTOMERS[@]}"; do
    batch_members+=("$entry")
    if [[ ${#batch_members[@]} -ge $BATCH ]]; then
        flush_batch || exit 1
    fi
done
flush_batch || exit 1
echo "Rollout complete, failures: ${fail}"
