#!/usr/bin/env bash
# Re-apply XFS project limits for all provisioned customers. Run as root.
# Tags persist in the filesystem, but limits are only enforced while /
# is mounted with pquota/prjquota, so after the first reboot with accounting
# ON every customer needs its `limit -p` re-applied once. Provision applies
# this automatically for new customers; this script covers boot + existing
# accounts (custom --quota-gb values are NOT stored in the registry, so they
# are re-applied with defaults here and must be re-set manually if customized).
# Idempotent. Triggered by systemd/sandpod-quotas.service at boot.
set -euo pipefail
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if ! grep -P '\s/\sxfs' /proc/mounts | grep -qE "pquota|prjquota"; then
    echo "PENDING: / has no pquota accounting; limits cannot apply yet." >&2
    echo "Run enable-project-quota.sh + reboot first." >&2
    exit 3
fi

# All known service users: registry column 2 + /etc/projid names.
USERS="$(awk -F'\t' '!/^\s*(#|$)/{print $2}' /etc/sandpod/customers.tsv 2>/dev/null || true)"
if [[ -f /etc/projid ]]; then
    USERS="$(printf '%s\n%s' "$USERS" "$(awk -F: '{print $2}' /etc/projid 2>/dev/null || true)")"
fi
USERS="$(printf '%s\n' "$USERS" | sed '/^\s*$/d' | sort -u || true)"
[[ -n "$USERS" ]] || { echo "No customers found; nothing to do."; exit 0; }

FAIL=0
while IFS= read -r u; do
    [[ -n "$u" ]] || continue
    if ! "${SCRIPT_DIR}/set-customer-quota.sh" "$u"; then
        echo "FAILED: $u" >&2
        FAIL=1
    fi
done <<< "$USERS"

exit "$FAIL"
