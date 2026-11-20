#!/usr/bin/env bash
# Install shared Python runtimes on the host (parallel to shared PHP/Node).
#
# Interim design ( AlmaLinux 10 container == AlmaLinux 10 host, same ABI ):
#   host RPMs provide /usr/bin/python3.12|3.13|3.14 + /usr/lib64/python3.* +
#   /usr/lib/python3.* ; customer containers mount those exact paths read-only.
#   Full /opt/sandpod/python/<ver> prefix builds are a later step (see TODO).
# Run as root. Idempotent.
set -euo pipefail

[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

dnf install -y python3 python3.13 python3.14

if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
    if ! command -v semanage >/dev/null; then
        echo "Install policycoreutils-python-utils to label shared runtime paths." >&2
        exit 1
    fi
    for path in '/usr/bin/python3\.12' '/usr/bin/python3\.13' '/usr/bin/python3\.14' '/usr/lib64/python3\.12(/.*)?' '/usr/lib64/python3\.13(/.*)?' '/usr/lib64/python3\.14(/.*)?' '/usr/lib/python3\.12(/.*)?' '/usr/lib/python3\.13(/.*)?' '/usr/lib/python3\.14(/.*)?'; do
        semanage fcontext -a -t container_file_t "$path" \
            || semanage fcontext -m -t container_file_t "$path"
    done
    restorecon -RF /usr/bin/python3.12 /usr/bin/python3.13 /usr/bin/python3.14 \
        /usr/lib64/python3.12 /usr/lib64/python3.13 /usr/lib64/python3.14 \
        /usr/lib/python3.12 /usr/lib/python3.13 /usr/lib/python3.14 2>/dev/null || true
fi

echo "---"
python3.12 --version
python3.13 --version
python3.14 --version
cat <<'NOTICE'
Interim mounts for quadlet (read-only, host -> container, same paths):
  /usr/bin/python3.12, /usr/bin/python3.13, /usr/bin/python3.14
  /usr/lib64/python3.12, /usr/lib64/python3.13, /usr/lib64/python3.14
  /usr/lib/python3.12, /usr/lib/python3.13, /usr/lib/python3.14
TODO: build self-contained /opt/sandpod/python/<ver> prefixes so the
container no longer depends on host /usr layout.
NOTICE
