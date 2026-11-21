#!/bin/sh
set -eu
umask 077
if [ "$(id -u)" = "0" ]; then
  mkdir -p "${DATA_DIR:-/data}"
  chown nest:nest "${DATA_DIR:-/data}"
  socket="${DOCKER_SOCKET:-/var/run/docker.sock}"
  gid=10001
  if [ -S "$socket" ]; then gid="$(stat -c '%g' "$socket")"; fi
  exec su-exec "10001:$gid" /app/panel "$@"
fi
exec /app/panel "$@"
