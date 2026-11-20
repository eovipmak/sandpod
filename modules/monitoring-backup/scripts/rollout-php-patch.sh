#!/usr/bin/env bash
# Fleet PHP patch rollout: update host RPMs, then reload each customer's
# affected FPM minor in batches with health-checks.
# Run as root. Stops on first batch failure (fix forward, then re-run).
#
# Usage: rollout-php-patch.sh [--batch-size 5] [--sleep-seconds 10]
#   [--minor 8.3]... [--skip-update] [--registry /etc/sandpod/customers.tsv]
#
# Default: all minors 8.2/8.3/8.4/8.5, batch-size 5.
set -euo pipefail

BATCH=5; SLEEP_S=10; SKIP_UPDATE=0
MINORS=()
REGISTRY="/etc/sandpod/customers.tsv"
while [[ $# -gt 0 ]]; do case "$1" in
  --batch-size) BATCH="${2:-}"; shift 2 ;;
  --sleep-seconds) SLEEP_S="${2:-}"; shift 2 ;;
  --minor) MINORS+=("${2:-}"); shift 2 ;;
  --skip-update) SKIP_UPDATE=1; shift ;;
  --registry) REGISTRY="${2:-}"; shift 2 ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RELOAD="${REPO_ROOT}/modules/shared-runtimes/scripts/reload-customer-php.sh"
HEALTH="${REPO_ROOT}/modules/customer-provisioning/scripts/health-check.sh"

[[ ${#MINORS[@]} -gt 0 ]] || MINORS=(8.2 8.3 8.4 8.5)
[[ -f "$REGISTRY" ]] || { echo "Missing $REGISTRY." >&2; exit 1; }

if [[ "$SKIP_UPDATE" -eq 0 ]]; then
    echo "== Updating host PHP RPMs =="
    "${REPO_ROOT}/modules/shared-runtimes/scripts/update-php-runtimes.sh"
else
    echo "== Skipping host RPM update (--skip-update) =="
fi

mapfile -t CUSTOMERS < <(grep -vP '^\s*(#|$)' "$REGISTRY")
[[ ${#CUSTOMERS[@]} -gt 0 ]] || { echo "Registry empty." >&2; exit 1; }
echo "== Rolling out to ${#CUSTOMERS[@]} customers, batch-size ${BATCH}, minors: ${MINORS[*]} =="

fail=0; batch_n=0; batch_members=()
flush_batch() {
    [[ ${#batch_members[@]} -eq 0 ]] && return 0
    batch_n=$((batch_n+1))
    echo "--- Batch ${batch_n}: ${batch_members[*]} ---"
    for entry in "${batch_members[@]}"; do
        IFS=$'\t' read -r inst user cont web _ssh _img <<<"$entry"
        for minor in "${MINORS[@]}"; do
            echo "Reload ${cont} php${minor} ..."
            if ! "$RELOAD" "$user" "$cont" "$minor"; then
                echo "FAIL reload ${cont} php${minor}" >&2; fail=1
            fi
        done
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
