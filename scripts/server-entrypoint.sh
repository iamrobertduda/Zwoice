#!/bin/sh
set -eu
# Railway mounts volumes as root. Grant access once, then drop all privileges.
mkdir -p "$ZWOICE_DATA_DIR"
chown zwoice:zwoice "$ZWOICE_DATA_DIR"
chmod 700 "$ZWOICE_DATA_DIR"
exec su-exec zwoice:zwoice zwoice server "$@"
