#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
    echo "Run the runtime updater as root." >&2
    exit 1
fi

dnf upgrade -y \
    'php82-*' \
    'php83-*' \
    'php84-*' \
    'php85-*' \
    oniguruma5php

# Optional like the installer: no php86 on hosts installed before Remi
# published it must not fail the fleet patch rollout.
dnf upgrade -y 'php86-*' || echo "WARNING: php86-* not available; 8.2-8.5 updated." >&2

if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
    restorecon -RF /opt/remi /etc/opt/remi /usr/lib64/libonig.so.105
fi

cat <<'NOTICE'
PHP RPMs are updated. Running FPM workers still use their loaded runtime.
Gracefully reload the affected PHP-FPM minor version in each customer container.
NOTICE
