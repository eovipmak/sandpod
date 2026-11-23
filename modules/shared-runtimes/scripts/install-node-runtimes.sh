#!/usr/bin/env bash
# Install shared Node.js runtimes on the host (parallel to shared PHP).
# Layout: /opt/sandpod/node/{22,24,26} (stable symlinks for containers),
#   /opt/sandpod/node/default -> 22. Customer containers mount
#   /opt/sandpod/node read-only; image symlinks /usr/local/bin/node* there.
#
# Management: fnm (https://github.com/Schniz/fnm) installs the exact patch
#   versions into FNM_DIR=/opt/sandpod/node/.fnm, then <major> symlinks point
#   at the fnm installation dir. Patch upgrades keep container paths stable
#   (/opt/sandpod/node/22/bin/node never moves from the panel's view).
#   Pass --no-fnm to install from official linux-x64 tarballs instead
#   (same final layout, no fnm dependency).
#
# Run as root. Idempotent (re-links when the wanted version differs).
#
# Usage: install-node-runtimes.sh [--node22-ver 22.19.0] [--node24-ver 24.13.0]
#   [--node26-ver 26.2.0] [--prefix /opt/sandpod/node] [--no-fnm]
#   [--fnm-ver 1.38.0]
set -euo pipefail

[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

NODE22_VER="22.19.0"
NODE24_VER="24.13.0"
NODE26_VER="26.2.0"
PREFIX="/opt/sandpod/node"
USE_FNM=1
FNM_VER="1.38.0"

while [[ $# -gt 0 ]]; do case "$1" in
  --node22-ver) NODE22_VER="${2:-}"; shift 2 ;;
  --node24-ver) NODE24_VER="${2:-}"; shift 2 ;;
  --node26-ver) NODE26_VER="${2:-}"; shift 2 ;;
  --prefix) PREFIX="${2:-}"; shift 2 ;;
  --no-fnm) USE_FNM=0; shift ;;
  --fnm-ver) FNM_VER="${2:-}"; shift 2 ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done

need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing required tool: $1" >&2; exit 1; }; }

# Official linux-x64 builds of Node 26+ need libatomic at runtime.
# AlmaLinux hosts get it from AppStream; install it when it is missing.
if ! ldconfig -p 2>/dev/null | grep -q libatomic; then
    if command -v dnf >/dev/null 2>&1; then
        echo "libatomic not found, installing via dnf ..."
        dnf install -y libatomic || echo "WARNING: libatomic install failed; node26 may not start." >&2
    else
        echo "WARNING: libatomic not found and no dnf to install it; node26 may not start." >&2
    fi
fi

verify_node() {
    local major="$1"
    "${PREFIX}/${major}/bin/node" --version >/dev/null || {
        echo "ERROR: ${PREFIX}/${major}/bin/node does not run (missing libatomic? try: dnf install -y libatomic)." >&2
        exit 1
    }
}

FNM_DIR="${PREFIX}/.fnm"
export FNM_DIR
FNM_BIN="/usr/local/bin/fnm"

install_fnm() {
    if [[ -x "$FNM_BIN" ]]; then
        echo "fnm present: $("$FNM_BIN" --version)"
        return 0
    fi
    need curl; need unzip
    local arch tmp
    arch="$(uname -m)"
    case "$arch" in
        x86_64) arch="x64" ;;
        aarch64) arch="arm64" ;;
        *) echo "Unsupported arch for fnm: $arch (use --no-fnm)." >&2; exit 1 ;;
    esac
    tmp="$(mktemp -d /var/tmp/sandpod-fnm-XXXXXX)"
    trap 'rm -rf "$tmp"' RETURN
    echo "Fetching fnm v${FNM_VER} ..."
    curl -fsSL "https://github.com/Schniz/fnm/releases/download/v${FNM_VER}/fnm-linux.zip" \
        -o "${tmp}/fnm.zip"
    unzip -o -q "${tmp}/fnm.zip" -d "$tmp"
    install -m 0755 "${tmp}/fnm" "$FNM_BIN"
    rm -rf "$tmp"
    trap - RETURN
    echo "Installed fnm: $("$FNM_BIN" --version)"
}

fnm_install_one() {
    local major="$1" ver="$2"
    local dest="${PREFIX}/${major}"
    # Already at the wanted version? (fnm dir + stable link both agree)
    if [[ -x "${dest}/bin/node" ]]; then
        local cur
        cur="$("${dest}/bin/node" --version 2>/dev/null || echo none)"
        if [[ "$cur" == "v${ver}" ]]; then
            echo "node${major} present: ${cur} at ${dest}"
            return 0
        fi
        echo "node${major} at ${cur}, want v${ver} - reinstalling ..."
    fi
    echo "fnm installing node v${ver} (major ${major}) ..."
    "$FNM_BIN" install "$ver"
    # fnm layout: $FNM_DIR/node-versions/v<ver>/installation
    local src="${FNM_DIR}/node-versions/v${ver}/installation"
    [[ -x "${src}/bin/node" ]] || { echo "fnm install missing: ${src}/bin/node" >&2; exit 1; }
    ln -sfn "$src" "$dest"
    local got
    got="$("${dest}/bin/node" --version)" || { echo "ERROR: ${dest}/bin/node does not run (missing libatomic?)." >&2; exit 1; }
    echo "node${major}: ${got} -> $(readlink "$dest")"
}

tarball_install_one() {
    local major="$1" ver="$2"
    local dest="${PREFIX}/${major}"
    if [[ -x "${dest}/bin/node" ]] && [[ "$("${dest}/bin/node" --version)" == "v${ver}" ]]; then
        echo "node${major} present: v${ver} at ${dest}"
        return 0
    fi
    need curl; need tar; need xz
    local url="https://nodejs.org/dist/v${ver}/node-v${ver}-linux-x64.tar.xz"
    local tmp
    tmp="$(mktemp -d /var/tmp/sandpod-node-${major}-XXXXXX)"
    trap 'rm -rf "$tmp"' RETURN
    echo "Fetching node v${ver} -> ${dest} ..."
    curl -fsSL "$url" -o "${tmp}/node.tar.xz"
    rm -rf "$dest"
    mkdir -p "$dest"
    tar -xJf "${tmp}/node.tar.xz" -C "$tmp"
    cp -a "${tmp}/node-v${ver}-linux-x64/." "$dest/"
    rm -rf "$tmp"
    trap - RETURN
    local got
    got="$("${dest}/bin/node" --version)" || { echo "ERROR: ${dest}/bin/node does not run (missing libatomic?)." >&2; exit 1; }
    echo "Installed node${major}: ${got}"
}

mkdir -p "$PREFIX"
if [[ "$USE_FNM" -eq 1 ]]; then
    install_fnm
    mkdir -p "$FNM_DIR"
    fnm_install_one 22 "$NODE22_VER"
    fnm_install_one 24 "$NODE24_VER"
    fnm_install_one 26 "$NODE26_VER"
else
    tarball_install_one 22 "$NODE22_VER"
    tarball_install_one 24 "$NODE24_VER"
    tarball_install_one 26 "$NODE26_VER"
fi
ln -sfn "22" "${PREFIX}/default"

label_selinux() {
    if command -v getenforce >/dev/null && [[ "$(getenforce)" == "Enforcing" ]]; then
        if ! command -v semanage >/dev/null; then
            echo "Install policycoreutils-python-utils to label shared runtime paths." >&2
            exit 1
        fi
        semanage fcontext -a -t container_file_t "${PREFIX}(/.*)?" \
            || semanage fcontext -m -t container_file_t "${PREFIX}(/.*)?"
        restorecon -RF "$PREFIX"
    fi
}
label_selinux

echo "---"
verify_node 22
verify_node 24
verify_node 26
"${PREFIX}/22/bin/node" --version
"${PREFIX}/24/bin/node" --version
"${PREFIX}/26/bin/node" --version
cat <<'NOTICE'
Next: mount /opt/sandpod/node read-only into customer containers
(see quadlet/customer@.container) and rebuild the image so
/usr/local/bin/node* resolves there (fallback: image nodejs RPMs).
Symlinks answer "symlink thì sao?": fnm owns .fnm/, stable
/opt/sandpod/node/<major> links hide fnm's versioned dirs from the
panel - patch upgrades never change container-visible paths.
NOTICE
