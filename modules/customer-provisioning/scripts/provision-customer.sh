#!/usr/bin/env bash
# Provision a new SandPod customer (rootless service account + Quadlet + data/config).
# Run as root. Idempotent for existing users/dirs; refuses to overwrite ports in use
# unless --force is given.
#
# Usage:
#   provision-customer.sh <instance-id> [--service-user svc-c002] [--web-port 18002]
#     [--ssh-port 22202] [--ssh-key /path/to/id_rsa.pub]
#     [--image localhost/sandpod/customer-runtime:pilot] [--image-tar /tmp/img.tar]
#     [--image-source-user svc-c001] [--skip-image-transfer] [--quota-gb 10]
#     [--skip-start] [--force]
#
# instance-id: 3-digit id, e.g. 001, 002. Container becomes customer-<id>,
#   unit customer@<id>.service.
set -euo pipefail

usage() {
    sed -n '2,12p' "$0"
    exit "${1:-2}"
}

INSTANCE="${1:-}"
[[ -n "$INSTANCE" ]] || usage 2
shift
[[ "$INSTANCE" =~ ^[0-9]{3}$ ]] || { echo "instance-id must be 3 digits (e.g. 002)." >&2; exit 2; }

SERVICE_USER=""
WEB_PORT=""
SSH_PORT=""
SSH_KEY=""
IMAGE="localhost/sandpod/customer-runtime:pilot"
IMAGE_TAR=""
IMAGE_SOURCE_USER=""
SKIP_IMAGE_TRANSFER=0
QUOTA_GB=10
SKIP_START=0
FORCE=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --service-user) SERVICE_USER="${2:-}"; shift 2 ;;
        --web-port) WEB_PORT="${2:-}"; shift 2 ;;
        --ssh-port) SSH_PORT="${2:-}"; shift 2 ;;
        --ssh-key) SSH_KEY="${2:-}"; shift 2 ;;
        --image) IMAGE="${2:-}"; shift 2 ;;
        --image-tar) IMAGE_TAR="${2:-}"; shift 2 ;;
        --image-source-user) IMAGE_SOURCE_USER="${2:-}"; shift 2 ;;
        --skip-image-transfer) SKIP_IMAGE_TRANSFER=1; shift ;;
        --quota-gb) QUOTA_GB="${2:-}"; shift 2 ;;
        --skip-start) SKIP_START=1; shift ;;
        --force) FORCE=1; shift ;;
        -h|--help) usage 0 ;;
        *) echo "Unknown arg: $1" >&2; usage 2 ;;
    esac
done

if [[ "${EUID}" -ne 0 ]]; then
    echo "Run as root." >&2; exit 1
fi

[[ -z "$SERVICE_USER" ]] && SERVICE_USER="svc-c${INSTANCE}"
[[ "$SERVICE_USER" =~ ^[a-z_][a-z0-9_-]*[$]?$ ]] || { echo "Invalid service user." >&2; exit 2; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${MODULE_DIR}/../.." && pwd)"
TEMPLATE_SRC="${REPO_ROOT}/modules/customer-container/quadlet/customer@.container"
NGINX_SRC="${REPO_ROOT}/modules/customer-container/config/nginx.conf"
INDEX_SRC="${REPO_ROOT}/modules/customer-container/config/default-index.html"
PILOT_PHP="${REPO_ROOT}/modules/customer-container/examples/pilot-site/index.php"
REGISTRY="/etc/sandpod/customers.tsv"

[[ -f "$TEMPLATE_SRC" ]] || { echo "Missing template: $TEMPLATE_SRC" >&2; exit 1; }
[[ -f "$NGINX_SRC" ]] || { echo "Missing nginx template: $NGINX_SRC" >&2; exit 1; }

port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltnH 2>/dev/null | awk '{print $4}' | grep -q ":${port}$" && return 0 || return 1
    fi
    (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null && return 0 || return 1
}

# Auto-allocate loopback ports when not given: web 18001+, ssh 22201+.
# Re-provisioning the same instance reuses its existing ports/registry entry.
EXISTING_WEB=""; EXISTING_SSH=""
if [[ -f "/etc/sandpod/customers.tsv" ]]; then
    EXISTING_LINE="$(awk -F'\t' -v id="${INSTANCE}" '$1==id{print}' /etc/sandpod/customers.tsv | tail -1 || true)"
    if [[ -n "${EXISTING_LINE:-}" ]]; then
        EXISTING_WEB="$(printf '%s' "$EXISTING_LINE" | cut -f4)"
        EXISTING_SSH="$(printf '%s' "$EXISTING_LINE" | cut -f5)"
    fi
fi
# Existing drop-in wins (it is what the unit actually publishes).
# NOTE: HOME_DIR is not known yet here for a brand-new user, so resolve from
# the default or existing account home.
_GUESS_HOME="$(getent passwd "${SERVICE_USER}" | cut -d: -f6 || true)"
if [[ -z "$_GUESS_HOME" ]]; then _GUESS_HOME="/home/${SERVICE_USER}"; fi
if [[ -f "${_GUESS_HOME}/.config/containers/systemd/customer@${INSTANCE}.container.d/10-instance.conf" ]]; then
    _w="$(grep -oP 'PublishPort=127\.0\.0\.1:\K[0-9]+(?=:80)' "${_GUESS_HOME}/.config/containers/systemd/customer@${INSTANCE}.container.d/10-instance.conf" | head -1 || true)"
    _s="$(grep -oP 'PublishPort=127\.0\.0\.1:\K[0-9]+(?=:22)' "${_GUESS_HOME}/.config/containers/systemd/customer@${INSTANCE}.container.d/10-instance.conf" | head -1 || true)"
    [[ -n "$_w" ]] && EXISTING_WEB="$_w"
    [[ -n "$_s" ]] && EXISTING_SSH="$_s"
fi
if [[ -z "$WEB_PORT" ]]; then
    if [[ -n "$EXISTING_WEB" ]]; then WEB_PORT="$EXISTING_WEB"; fi
fi
if [[ -z "$SSH_PORT" ]]; then
    if [[ -n "$EXISTING_SSH" ]]; then SSH_PORT="$EXISTING_SSH"; fi
fi
if [[ -z "$WEB_PORT" ]]; then
    WEB_PORT=18001
    n=$((${INSTANCE} + 18000))
    if [[ "$n" -ge "$WEB_PORT" ]]; then WEB_PORT="$n"; fi
    while port_in_use "$WEB_PORT" || grep -qP "\t${WEB_PORT}\t" "$REGISTRY" 2>/dev/null; do WEB_PORT=$((WEB_PORT+1)); done
fi
if [[ -z "$SSH_PORT" ]]; then
    SSH_PORT=22201
    n=$((${INSTANCE} + 22200))
    if [[ "$n" -ge "$SSH_PORT" ]]; then SSH_PORT="$n"; fi
    while port_in_use "$SSH_PORT" || grep -qP "\t${SSH_PORT}$" "$REGISTRY" 2>/dev/null; do SSH_PORT=$((SSH_PORT+1)); done
fi

[[ "$WEB_PORT" =~ ^[0-9]+$ ]] && [[ "$SSH_PORT" =~ ^[0-9]+$ ]] || { echo "Ports must be numeric." >&2; exit 2; }
[[ "$WEB_PORT" != "$SSH_PORT" ]] || { echo "Web and SSH ports must differ." >&2; exit 2; }

if [[ "$FORCE" -eq 0 ]]; then
    if port_in_use "$WEB_PORT"; then echo "Web port ${WEB_PORT} is in use. Pass --force to override." >&2; exit 1; fi
    if port_in_use "$SSH_PORT"; then echo "SSH port ${SSH_PORT} is in use. Pass --force to override." >&2; exit 1; fi
fi

if [[ -n "$SSH_KEY" && ! -f "$SSH_KEY" ]]; then echo "SSH key not found: $SSH_KEY" >&2; exit 2; fi

CONTAINER="customer-${INSTANCE}"
echo "Provisioning instance=${INSTANCE} user=${SERVICE_USER} container=${CONTAINER} web=127.0.0.1:${WEB_PORT} ssh=127.0.0.1:${SSH_PORT}"

# 1. Service account with no host shell.
if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    useradd --create-home --shell /sbin/nologin "$SERVICE_USER"
    echo "Created user ${SERVICE_USER}"
else
    echo "User ${SERVICE_USER} exists, reusing."
fi
HOME_DIR="$(getent passwd "$SERVICE_USER" | cut -d: -f6)"
[[ -n "$HOME_DIR" ]] || { echo "Cannot resolve home for ${SERVICE_USER}" >&2; exit 1; }

# 2. Lingering user manager so rootless units survive logout.
loginctl enable-linger "$SERVICE_USER"

# 3. Quadlet units.
UNIT_DIR="${HOME_DIR}/.config/containers/systemd"
DROPIN_DIR="${UNIT_DIR}/customer@${INSTANCE}.container.d"
install -d -m 0755 "${HOME_DIR}/.config" "${HOME_DIR}/.config/containers" "$UNIT_DIR" "$DROPIN_DIR"
chown -R "${SERVICE_USER}:${SERVICE_USER}" "${HOME_DIR}/.config"
install -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0644 "$TEMPLATE_SRC" "${UNIT_DIR}/customer@.container"
ln -sfn "customer@.container" "${UNIT_DIR}/customer@${INSTANCE}.container"
chown -h "$SERVICE_USER:$SERVICE_USER" "${UNIT_DIR}/customer@${INSTANCE}.container"
# Pin image per instance without editing the shared template.
cat > "${DROPIN_DIR}/10-instance.conf" <<EOF
[Container]
Image=${IMAGE}
PublishPort=127.0.0.1:${WEB_PORT}:80
PublishPort=127.0.0.1:${SSH_PORT}:22
EOF
chown "$SERVICE_USER:$SERVICE_USER" "${DROPIN_DIR}/10-instance.conf"
chmod 0644 "${DROPIN_DIR}/10-instance.conf"

# 4. Data + config. Control panel owns ~/data layout afterwards.
install -d -m 0755 "${HOME_DIR}/data" "${HOME_DIR}/data/public_html" "${HOME_DIR}/config"
chown -R "${SERVICE_USER}:${SERVICE_USER}" "${HOME_DIR}/data" "${HOME_DIR}/config"
if [[ ! -f "${HOME_DIR}/config/nginx.conf" ]]; then
    install -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0644 "$NGINX_SRC" "${HOME_DIR}/config/nginx.conf"
    echo "Installed config/nginx.conf template"
else
    echo "config/nginx.conf exists, keeping."
fi
if [[ ! -f "${HOME_DIR}/data/public_html/index.html" && -f "$INDEX_SRC" ]]; then
    install -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0644 "$INDEX_SRC" "${HOME_DIR}/data/public_html/index.html"
fi
if [[ ! -f "${HOME_DIR}/data/public_html/index.php" && -f "$PILOT_PHP" ]]; then
    install -o "$SERVICE_USER" -g "$SERVICE_USER" -m 0644 "$PILOT_PHP" "${HOME_DIR}/data/public_html/index.php"
fi

# 5. Start the user service.
UID_NUM="$(id -u "$SERVICE_USER")"
user_systemctl() {
    runuser -u "$SERVICE_USER" -- env HOME="$HOME_DIR" \
        XDG_RUNTIME_DIR="/run/user/${UID_NUM}" \
        DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${UID_NUM}/bus" \
        systemctl --user "$@"
}
user_podman() {
    runuser -u "$SERVICE_USER" -- env HOME="$HOME_DIR" \
        XDG_RUNTIME_DIR="/run/user/${UID_NUM}" \
        DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/${UID_NUM}/bus" \
        podman "$@"
}

# Rootless storage is per-account: a localhost image built as one customer is
# invisible to the next. Transfer it via save/load unless the target already
# has it (registry images pull fine on first start, so this is best-effort).
ensure_image() {
    if [[ "$SKIP_IMAGE_TRANSFER" -eq 1 ]]; then return 0; fi
    if user_podman image exists "$IMAGE" >/dev/null 2>&1; then
        echo "Image ${IMAGE} already present for ${SERVICE_USER}"
        return 0
    fi
    if [[ -n "$IMAGE_TAR" ]]; then
        [[ -f "$IMAGE_TAR" ]] || { echo "Image tar not found: $IMAGE_TAR" >&2; return 1; }
        echo "Loading image from ${IMAGE_TAR} ..."
        user_podman load -i "$IMAGE_TAR"
        return 0
    fi
    # Find a donor account that already has the image.
    donors=()
    if [[ -n "$IMAGE_SOURCE_USER" ]]; then donors+=("$IMAGE_SOURCE_USER"); fi
    if [[ -f "$REGISTRY" ]]; then
        while IFS=$'\t' read -r _ri donor _rest; do
            [[ "$_ri" =~ ^#.*$ || -z "$_ri" ]] && continue
            [[ "$donor" != "$SERVICE_USER" ]] && donors+=("$donor")
        done < "$REGISTRY"
    fi
    # Fall back to known pilot account.
    donors+=("svc-c001")
    # Deduplicate.
    seen=" "
    uniq_donors=()
    for d in "${donors[@]}"; do
        [[ "$seen" == *" $d "* ]] && continue
        seen+=" $d "
        uniq_donors+=("$d")
    done
    for donor in "${uniq_donors[@]}"; do
        id "$donor" >/dev/null 2>&1 || continue
        [[ "$donor" == "$SERVICE_USER" ]] && continue
        donor_home="$(getent passwd "$donor" | cut -d: -f6)"
        donor_uid="$(id -u "$donor")"
        if [[ ! -d "/run/user/${donor_uid}" ]]; then continue; fi
        if ! runuser -u "$donor" -- env HOME="$donor_home" \
                XDG_RUNTIME_DIR="/run/user/${donor_uid}" \
                podman image exists "$IMAGE" >/dev/null 2>&1; then continue; fi
        echo "Transferring ${IMAGE} from ${donor} to ${SERVICE_USER} ..."
        tmp_tar="$(mktemp /var/tmp/sandpod-image-${INSTANCE}-XXXXXX.tar)"
        chown "${donor}:${donor}" "$tmp_tar"
        chmod 0644 "$tmp_tar"
        runuser -u "$donor" -- env HOME="$donor_home" \
            XDG_RUNTIME_DIR="/run/user/${donor_uid}" \
            podman save -o "$tmp_tar" "$IMAGE"
        chmod 0644 "$tmp_tar"
        user_podman load -i "$tmp_tar"
        rm -f "$tmp_tar"
        return 0
    done
    echo "WARNING: image ${IMAGE} not found locally; unit start will try to pull it." >&2
    return 0
}
ensure_image
user_systemctl daemon-reload
if [[ "$SKIP_START" -eq 0 ]]; then
    user_systemctl restart "customer@${INSTANCE}.service"
    # Quadlet generates units dynamically; enable is best-effort.
    user_systemctl enable "customer@${INSTANCE}.service" >/dev/null 2>&1 || \
        echo "NOTE: enable skipped (generated unit); service started."
    echo "Started customer@${INSTANCE}.service"
else
    echo "Skipped start (--skip-start)."
fi

# 6. SSH key injection (container's /home/customer is ephemeral, so install after start).
if [[ -n "$SSH_KEY" && "$SKIP_START" -eq 0 ]]; then
    "${SCRIPT_DIR}/add-ssh-key.sh" "$SERVICE_USER" "$CONTAINER" "$SSH_KEY"
fi

# 7. Registry for fleet helpers (update/reload/monitoring).
install -d -m 0755 /etc/sandpod
if [[ ! -f "$REGISTRY" ]]; then
    printf '#instance\tservice_user\tcontainer\tweb_port\tssh_port\timage\n' > "$REGISTRY"
    chmod 0644 "$REGISTRY"
fi
grep -v -P "^${INSTANCE}\t" "$REGISTRY" > "${REGISTRY}.tmp" || true
printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$INSTANCE" "$SERVICE_USER" "$CONTAINER" "$WEB_PORT" "$SSH_PORT" "$IMAGE" >> "${REGISTRY}.tmp"
mv "${REGISTRY}.tmp" "$REGISTRY"

# 8. Disk quota (best-effort: pending until XFS pquota is active after reboot).
if [[ "$QUOTA_GB" =~ ^[0-9]+$ && "$QUOTA_GB" -gt 0 ]]; then
    SOFT_GB=$(( QUOTA_GB * 8 / 10 ))
    [[ "$SOFT_GB" -ge 1 ]] || SOFT_GB=1
    "${REPO_ROOT}/modules/host-storage/scripts/set-customer-quota.sh" \
        "$SERVICE_USER" --hard-gb "$QUOTA_GB" --soft-gb "$SOFT_GB" || \
        echo "NOTE: quota not enforced yet (see host-storage README)." >&2
fi

echo "Done. Verify with:"
echo "  ${SCRIPT_DIR}/health-check.sh ${WEB_PORT}"
