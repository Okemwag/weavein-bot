#!/bin/sh
set -eu

if [ "$#" -ne 1 ] || [ -z "${RESTORE_DATABASE_URL:-}" ]; then
    echo 'usage: RESTORE_DATABASE_URL=... scripts/restore-db.sh /secure/path/memberos.dump' >&2
    exit 2
fi

backup_path=$1
if [ ! -f "$backup_path" ]; then
    echo 'backup file does not exist' >&2
    exit 2
fi
pg_restore --list "$backup_path" >/dev/null

# A restore is allowed only into a freshly created, empty public schema.
table_count=$(psql "$RESTORE_DATABASE_URL" -X -A -t -v ON_ERROR_STOP=1 -c \
    "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'")
if [ "$table_count" != 0 ]; then
    echo 'restore target public schema is not empty' >&2
    exit 2
fi

pg_restore --dbname="$RESTORE_DATABASE_URL" --no-owner --no-privileges \
    --single-transaction --exit-on-error "$backup_path"
echo 'restore completed; verify application readiness before using this database'
