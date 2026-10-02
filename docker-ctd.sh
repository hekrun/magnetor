#!/bin/sh
set -e

[ $# -eq 0 ] && set -- help

# Run as the app user so files in /data keep the right owner.
if [ "$(id -u)" = "0" ]; then
  exec setpriv --reuid=10001 --regid=10001 --clear-groups /app/magnetor "$@"
fi

exec /app/magnetor "$@"
