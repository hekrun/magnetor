#!/bin/sh
set -e

# Docker creates a missing bind-mount source as root, so fix ownership before dropping privileges.
if [ "$(id -u)" = "0" ]; then
  mkdir -p /data/downloads /data/state
  for dir in /data /data/downloads /data/state; do
    if [ "$(stat -c %u "$dir")" != "10001" ]; then
      chown -R 10001:10001 "$dir"
    fi
  done
  exec setpriv --reuid=10001 --regid=10001 --clear-groups /app/magnetor "$@"
fi

exec /app/magnetor "$@"
