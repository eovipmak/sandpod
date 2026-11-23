#!/usr/bin/env bash
# Update shared Node.js runtimes to newer patches (parallel to update-php).
# Re-runs the installer: fnm installs the wanted versions, stable
# /opt/sandpod/node/<major> symlinks are re-pointed (container paths don't
# move from the panel's view). Existing port apps keep running the old
# binaries in memory - restart them per customer afterwards
# (see reload-customer-app.sh / rollout-app-patch.sh).
# Run as root.
#
# Usage: update-node-runtimes.sh [--node22-ver X] [--node24-ver Y]
#   [--node26-ver Z] [--prefix /opt/sandpod/node] [--no-fnm]
#   (no version flags = re-assert currently pinned versions)
set -euo pipefail

[[ "${EUID}" -eq 0 ]] || { echo "Run the runtime updater as root." >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PREFIX="/opt/sandpod/node"
ARGS=()
while [[ $# -gt 0 ]]; do case "$1" in
  --prefix) PREFIX="${2:-}"; ARGS+=("$1" "$2"); shift 2 ;;
  *) ARGS+=("$1"); shift ;;
esac; done

# No version flags: installer defaults pin the current versions (idempotent).
"${SCRIPT_DIR}/install-node-runtimes.sh" "${ARGS[@]}"

if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
    restorecon -RF "$PREFIX"
fi

cat <<'NOTICE'
Node trees are updated. Running port apps still use their loaded runtime.
Restart each affected port app in every customer container
(reload-customer-app.sh per domain, or rollout-app-patch.sh in batches).
NOTICE
