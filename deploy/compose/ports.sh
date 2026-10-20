#!/bin/sh
# Writes deploy/compose/.env with host ports for the Compose stack, once.
# Each default moves to the next free port if something on this machine
# already listens there. Delete .env to pick again.
set -eu

env_file="$(dirname "$0")/.env"
[ -f "$env_file" ] && exit 0

# A connect test sees listeners owned by any user (lsof without root doesn't).
busy() {
  if command -v nc >/dev/null 2>&1; then
    nc -z 127.0.0.1 "$1" >/dev/null 2>&1 && return 0
  fi
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1 && return 0
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -ltn "sport = :$1" | grep -q LISTEN && return 0
  fi
  return 1
}

# pick NAME DEFAULT sets NAME to the first port from DEFAULT that is free and
# not already picked (a subshell would lose the picked list, so no $(...)).
picked=""
pick() {
  p=$2
  while busy "$p" || case " $picked " in *" $p "*) true ;; *) false ;; esac; do
    p=$((p + 1))
  done
  picked="$picked $p"
  echo "$1=$p" >>"$env_file.tmp"
}

echo "# Host ports for deploy/compose (written by ports.sh; delete to re-pick)." >"$env_file.tmp"
pick KITMAKER_PORT 8080
pick POSTGRES_PORT 5432
pick MINIO_PORT 9000
pick MINIO_CONSOLE_PORT 9001
pick BUILDKIT_PORT 1234
pick ZOT_PORT 5000
pick MAILPIT_UI_PORT 8025
pick MAILPIT_SMTP_PORT 1025
mv "$env_file.tmp" "$env_file"
cat "$env_file"
