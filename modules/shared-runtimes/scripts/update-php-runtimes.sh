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

if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
    restorecon -RF /opt/remi /etc/opt/remi /usr/lib64/libonig.so.105
fi

cat <<'NOTICE'
PHP RPMs are updated. Running FPM workers still use their loaded runtime.
Gracefully reload the affected PHP-FPM minor version in each customer container.
NOTICE
