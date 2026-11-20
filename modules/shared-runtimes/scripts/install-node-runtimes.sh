#!/usr/bin/env bash
# Install shared Node.js runtimes on the host (parallel to shared PHP).
# Layout: /opt/sandpod/node/{22,24,26} (official linux-x64 tarballs),
#   /opt/sandpod/node/default -> 22. Customer containers mount
#   /opt/sandpod/node read-only; image symlinks node/node-*/npm* there.
# Run as root. Idempotent (skips versions already installed).
#
# Usage: install-node-runtimes.sh [--node22-ver 22.17.0] [--node24-ver 24.10.0]
#   [--node26-ver 26.4.0] [--prefix /opt/sandpod/node]
set -euo pipefail

[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

NODE22_VER="22.19.0"
NODE24_VER="24.13.0"
NODE26_VER="26.2.0"
PREFIX="/opt/sandpod/node"

while [[ $# -gt 0 ]]; do case "$1" in
  --node22-ver) NODE22_VER="${2:-}"; shift 2 ;;
  --node24-ver) NODE24_VER="${2:-}"; shift 2 ;;
  --node26-ver) NODE26_VER="${2:-}"; shift 2 ;;
  --prefix) PREFIX="${2:-}"; shift 2 ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done

need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing required tool: $1" >&2; exit 1; }; }
need curl; need tar; need xz

install_one() {
    local major="$1" ver="$2"
    local dest="${PREFIX}/${major}"
    if [[ -x "${dest}/bin/node" ]]; then
        echo "node${major} present: $("${dest}/bin/node" --version) at ${dest} (want v${ver}; pass --node${major}-ver to change)"
        return 0
    fi
    local url="https://nodejs.org/dist/v${ver}/node-v${ver}-linux-x64.tar.xz"
    local tmp
    tmp="$(mktemp -d /var/tmp/sandpod-node-${major}-XXXXXX)"
    trap 'rm -rf "$tmp"' RETURN
    echo "Fetching node v${ver} -> ${dest} ..."
    curl -fsSL "$url" -o "${tmp}/node.tar.xz"
    mkdir -p "$dest"
    tar -xJf "${tmp}/node.tar.xz" -C "$tmp"
    cp -a "${tmp}/node-v${ver}-linux-x64/." "$dest/"
    rm -rf "$tmp"
    trap - RETURN
    echo "Installed node${major}: $("${dest}/bin/node" --version)"
}

mkdir -p "$PREFIX"
install_one 22 "$NODE22_VER"
install_one 24 "$NODE24_VER"
install_one 26 "$NODE26_VER"
ln -sfn "22" "${PREFIX}/default"

if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
    if ! command -v semanage >/dev/null; then
        echo "Install policycoreutils-python-utils to label shared runtime paths." >&2
        exit 1
    fi
    semanage fcontext -a -t container_file_t "${PREFIX}(/.*)?" \
        || semanage fcontext -m -t container_file_t "${PREFIX}(/.*)?"
    restorecon -RF "$PREFIX"
fi

echo "---"
"${PREFIX}/22/bin/node" --version
"${PREFIX}/24/bin/node" --version
"${PREFIX}/26/bin/node" --version
cat <<'NOTICE'
Next: mount /opt/sandpod/node read-only into customer containers
(see quadlet/customer@.container) and rebuild the image so
/usr/local/bin/node* resolves there (fallback: image nodejs RPMs).
NOTICE
