#!/usr/bin/env bash
# Persist XFS project quota on / and report activation state. Run as root, idempotent.
# XFS project accounting only turns on at initial mount: remount cannot enable
# it (silently ignored on this kernel), so this script sets BOTH fstab
# (`defaults,pquota`, applied by systemd-remount-fs) AND the kernel boot
# parameter (`rootflags=pquota`, applied at initial mount). A reboot is
# required the first time. Safe to re-run.
set -euo pipefail
[[ "${EUID}" -eq 0 ]] || { echo "Run as root." >&2; exit 1; }

FSTAB="/etc/fstab"
UUID_LINE="$(grep -P '^\s*UUID=\S+\s+/\s+xfs\s' "$FSTAB" || true)"
[[ -n "$UUID_LINE" ]] || { echo "No xfs / entry in $FSTAB." >&2; exit 1; }

if grep -P '^\s*UUID=\S+\s+/\s+xfs\s+\S*pquota' "$FSTAB" >/dev/null; then
    echo "fstab already has pquota for /."
else
    cp -a "$FSTAB" "${FSTAB}.bak.$(date +%Y%m%d-%H%M%S)"
    # defaults -> defaults,pquota (keep any other existing options).
    python3 - "$FSTAB" <<'PYEOF'
import re, sys
p = sys.argv[1]
s = open(p).read()
def add(m):
    opts = m.group(1)
    if 'pquota' in opts.split(','):
        return m.group(0)
    return m.group(0).replace(opts, opts + ',pquota', 1)
s2, n = re.subn(r'(?m)^(UUID=\S+\s+/\s+xfs\s+)(\S+)', lambda m: m.group(1) + (m.group(2) + ',pquota' if 'pquota' not in m.group(2).split(',') else m.group(2)), s, count=1)
open(p, 'w').write(s2)
print('updated entries: %d' % n)
PYEOF
    systemctl daemon-reload 2>/dev/null || true
    echo "Persisted pquota in $FSTAB."
fi

# Belt and suspenders: the initial root mount comes from the kernel cmdline,
# and XFS ignores quota options on remount - so rootflags is what activates it.
if command -v grubby >/dev/null 2>&1; then
    if grubby --info=DEFAULT 2>/dev/null | grep -q 'rootflags=.*pquota'; then
        echo "Kernel cmdline already has rootflags=pquota."
    else
        grubby --update-kernel=ALL --args="rootflags=pquota"
        echo "Added rootflags=pquota to all boot entries."
    fi
else
    echo "WARNING: grubby not found - add rootflags=pquota to the kernel cmdline manually." >&2
fi

if grep -P '\s/\sxfs' /proc/mounts | grep -qE 'pquota|prjquota'; then
    echo "ACTIVE: / is mounted with project quota accounting."
    xfs_quota -x -c 'state -p' / 2>&1 || true
else
    echo "PENDING: / is not (yet) mounted with pquota."
    echo "Reboot to activate, then re-run this script to verify."
fi
