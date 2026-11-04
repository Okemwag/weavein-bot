#!/bin/sh
set -eu

if [ "$#" -ne 1 ] || [ -z "${DATABASE_URL:-}" ]; then
    echo 'usage: DATABASE_URL=... scripts/backup-db.sh /secure/path/memberos.dump' >&2
    exit 2
fi

backup_path=$1
if [ -e "$backup_path" ]; then
    echo 'backup destination already exists' >&2
    exit 2
fi

umask 077
temporary_path=$(mktemp "${backup_path}.tmp.XXXXXX")
trap 'rm -f "$temporary_path"' EXIT HUP INT TERM
pg_dump --dbname="$DATABASE_URL" --format=custom --no-owner --no-privileges --file="$temporary_path"
pg_restore --list "$temporary_path" >/dev/null
mv -- "$temporary_path" "$backup_path"
trap - EXIT HUP INT TERM
echo "backup saved: $backup_path"
