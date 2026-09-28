#!/bin/sh
# Use /data/cad-config.json (agents etc. for this host) when present, else the image's /app/cad/config.json.
# An explicit CAD_CONFIG always wins.
if [ -z "${CAD_CONFIG:-}" ] && [ -f /data/cad-config.json ]; then
  export CAD_CONFIG=/data/cad-config.json
fi
mkdir -p /data/cad/config /data/orchd/state 2>/dev/null || true
exec "$@"
