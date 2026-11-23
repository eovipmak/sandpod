#!/usr/bin/env bash
# Create busybox-style symlinks: <cmd> -> sandpodctl, so legacy
# `modules/.../scripts/<cmd>.sh` call sites keep working during migration.
# Usage: shims.sh [bindir=/usr/local/bin] [--dir <project-shim-dir>]
set -euo pipefail
BINDIR="${1:-/usr/local/bin}"
if [[ "${1:-}" == "--dir" ]]; then BINDIR="${2:-./shims}"; fi
BIN="sandpodctl"
SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/${BIN}"
[[ -x "$SRC" ]] || { echo "Build first: make -C $(dirname "$0") build" >&2; exit 1; }
CMDS=(provision-customer remove-customer add-ssh-key health-check
  add-site remove-site list-sites set-php-version add-port-app remove-port-app
  install-php-runtimes update-php-runtimes reload-customer-php
  install-node-runtimes update-node-runtimes install-python-runtimes update-python-runtimes reload-customer-app
  generate-site-conf reload-host-nginx
  create-site-db backup-site-db restore-site-db
  enable-project-quota quota-report reapply-quotas set-customer-quota
  backup-customer resource-report rollout-php-patch rollout-app-patch)
mkdir -p "$BINDIR"
for c in "${CMDS[@]}"; do
  ln -sfn "$SRC" "${BINDIR}/${c}"
  ln -sfn "$SRC" "${BINDIR}/${c}.sh"
done
echo "Shims installed in ${BINDIR} (${#CMDS[@]} commands x2 with .sh alias)."
