#!/usr/bin/env bash
# Test and reload host nginx. Run as root.
set -euo pipefail
nginx -t
systemctl reload nginx
echo "Host nginx reloaded."
