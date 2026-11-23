#!/usr/bin/env bash
# Install shared Python runtimes on the host (parallel to shared PHP/Node).
#
# Self-contained prefixes (no host /usr dependency):
#   /opt/sandpod/python/3.12, 3.13, 3.14 (stable symlinks for containers),
#   /opt/sandpod/python/default -> 3.12
# Customer containers mount /opt/sandpod/python read-only; image symlinks
# /usr/local/bin/python3.* there (fallback: image python RPMs).
#
# Management: uv (https://github.com/astral-sh/uv) installs the exact patch
#   versions into UV_PYTHON_INSTALL_DIR=/opt/sandpod/python/.uv, then <ver>
#   symlinks point at the uv-managed install dir. Patch upgrades keep
#   container paths stable (/opt/sandpod/python/3.12/bin/python3.12 never
#   moves from the panel's view).
#   Pass --no-uv to download python-build-standalone install_only tarballs
#   directly instead (same final layout, no uv dependency).
#
# Run as root. Idempotent (re-links when the wanted version differs).
#
# Usage: install-python-runtimes.sh [--py312-ver 3.12.7] [--py313-ver 3.13.7]
#   [--py314-ver 3.14.0] [--release-tag 20251014] [--prefix /opt/sandpod/python]
#   [--no-uv] [--uv-ver 0.10.0]
set -euo pipefail

[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

PY312_VER="3.12.7"
PY313_VER="3.13.7"
PY314_VER="3.14.0"
RELEASE_TAG="20251014" # only used with --no-uv (standalone release tag)
PREFIX="/opt/sandpod/python"
USE_UV=1
UV_VER="0.10.0"

while [[ $# -gt 0 ]]; do case "$1" in
  --py312-ver) PY312_VER="${2:-}"; shift 2 ;;
  --py313-ver) PY313_VER="${2:-}"; shift 2 ;;
  --py314-ver) PY314_VER="${2:-}"; shift 2 ;;
  --release-tag) RELEASE_TAG="${2:-}"; shift 2 ;;
  --prefix) PREFIX="${2:-}"; shift 2 ;;
  --no-uv) USE_UV=0; shift ;;
  --uv-ver) UV_VER="${2:-}"; shift 2 ;;
  *) echo "Unknown arg: $1" >&2; exit 2 ;;
esac; done
# --prefix becomes rm -rf / restorecon targets: refuse system paths early.
[[ "$PREFIX" == /* ]] || { echo "--prefix must be absolute." >&2; exit 2; }
case "$PREFIX" in
  /|/usr|/etc|/var|/home|/root|/boot|/proc|/sys|/dev|/run|/tmp|/bin|/sbin|/lib|/lib64)
    echo "Refusing --prefix ${PREFIX} (system path)." >&2; exit 2 ;;
esac
for v in "$PY312_VER" "$PY313_VER" "$PY314_VER" "$UV_VER"; do
    [[ "$v" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "Bad version: $v (want X.Y.Z)." >&2; exit 2; }
done
[[ "$RELEASE_TAG" =~ ^[0-9]+$ ]] || { echo "Bad --release-tag: $RELEASE_TAG." >&2; exit 2; }

need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing required tool: $1" >&2; exit 1; }; }

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) TARGET="x86_64-unknown-linux-gnu" ;;
  aarch64) TARGET="aarch64-unknown-linux-gnu" ;;
  *) echo "Unsupported arch: $ARCH" >&2; exit 1 ;;
esac

UV_DIR="${PREFIX}/.uv"
export UV_PYTHON_INSTALL_DIR="$UV_DIR"
UV_BIN="/usr/local/bin/uv"

install_uv() {
    if [[ -x "$UV_BIN" ]]; then
        echo "uv present: $("$UV_BIN" --version)"
        return 0
    fi
    need curl; need tar
    local tmp
    tmp="$(mktemp -d /var/tmp/sandpod-uv-XXXXXX)"
    trap 'rm -rf "$tmp"' RETURN
    echo "Fetching uv v${UV_VER} ..."
    curl -fsSL "https://github.com/astral-sh/uv/releases/download/${UV_VER}/uv-${TARGET}.tar.gz" \
        -o "${tmp}/uv.tar.gz"
    tar -xzf "${tmp}/uv.tar.gz" -C "$tmp"
    install -m 0755 "${tmp}/uv-${TARGET}/uv" "$UV_BIN"
    rm -rf "$tmp"
    trap - RETURN
    echo "Installed uv: $("$UV_BIN" --version)"
}

uv_install_one() {
    local ver="$1" # e.g. 3.12.7
    local minor="${ver%.*}" # e.g. 3.12
    local dest="${PREFIX}/${minor}"
    if [[ -x "${dest}/bin/python${minor}" ]]; then
        local cur
        cur="$("${dest}/bin/python${minor}" --version 2>&1 | awk '{print $2}')"
        if [[ "$cur" == "$ver" ]]; then
            echo "python${minor} present: ${cur} at ${dest}"
            return 0
        fi
        echo "python${minor} at ${cur}, want ${ver} - reinstalling ..."
    fi
    echo "uv installing CPython ${ver} ..."
    "$UV_BIN" python install "$ver"
    # uv layout: $UV_DIR/cpython-<ver>-<target>/bin/python3.<minor>
    local binpath
    binpath="$("$UV_BIN" python find "$ver" --python-preference only-managed)"
    local root
    root="$(dirname "$(dirname "$binpath")")"
    [[ -x "${root}/bin/python${minor}" ]] \
        || { echo "uv install missing: ${root}/bin/python${minor}" >&2; exit 1; }
    ln -sfn "$root" "$dest"
    echo "python${minor}: $("${dest}/bin/python${minor}" --version) -> $(readlink "$dest")"
}

tarball_install_one() {
    local ver="$1" # e.g. 3.12.7
    local minor="${ver%.*}" # e.g. 3.12
    local dest="${PREFIX}/${minor}"
    if [[ -x "${dest}/bin/python${minor}" ]]; then
        local cur
        cur="$("${dest}/bin/python${minor}" --version 2>&1 | awk '{print $2}')"
        if [[ "$cur" == "$ver" ]]; then
            echo "python${minor} present: ${cur} at ${dest}"
            return 0
        fi
        echo "python${minor} at ${cur}, want ${ver} - reinstalling ..."
    fi
    need curl; need tar
    local url="https://github.com/astral-sh/python-build-standalone/releases/download/${RELEASE_TAG}/cpython-${ver}+${RELEASE_TAG}-${TARGET}-install_only.tar.gz"
    local tmp
    tmp="$(mktemp -d /var/tmp/sandpod-python-${minor}-XXXXXX)"
    trap 'rm -rf "$tmp"' RETURN
    echo "Fetching CPython ${ver} -> ${dest} ..."
    curl -fsSL "$url" -o "${tmp}/python.tar.gz"
    # install_only tarballs unpack to ./python/... - move contents up.
    tar -xzf "${tmp}/python.tar.gz" -C "$tmp"
    # Verify the fresh tree BEFORE touching the live one (a failed
    # download/extract must never delete the running runtime).
    fresh="${tmp}/python/bin/python${minor}"
    [[ -x "$fresh" ]] || { echo "ERROR: extracted tree missing bin/python${minor}." >&2; exit 1; }
    "$fresh" --version >/dev/null || { echo "ERROR: fresh python ${ver} does not run." >&2; exit 1; }
    rm -rf "$dest"
    mkdir -p "$dest"
    cp -a "${tmp}/python/." "$dest/"
    rm -rf "$tmp"
    trap - RETURN
    echo "Installed python${minor}: $("${dest}/bin/python${minor}" --version)"
}

mkdir -p "$PREFIX"
if [[ "$USE_UV" -eq 1 ]]; then
    install_uv
    mkdir -p "$UV_DIR"
    uv_install_one "$PY312_VER"
    uv_install_one "$PY313_VER"
    uv_install_one "$PY314_VER"
else
    tarball_install_one "$PY312_VER"
    tarball_install_one "$PY313_VER"
    tarball_install_one "$PY314_VER"
fi
ln -sfn "3.12" "${PREFIX}/default"

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
"${PREFIX}/3.12/bin/python3.12" --version
"${PREFIX}/3.13/bin/python3.13" --version
"${PREFIX}/3.14/bin/python3.14" --version
cat <<'NOTICE'
Next: mount /opt/sandpod/python read-only into customer containers
(see quadlet/customer@.container) and rebuild the image so
/usr/local/bin/python3.* resolves there (fallback: image python RPMs).
Symlinks answer the same question as Node: uv owns .uv/, stable
/opt/sandpod/python/<ver> links hide uv's versioned dirs from the
panel - patch upgrades never change container-visible paths.
NOTICE
