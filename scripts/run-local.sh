#!/bin/sh
set -eu

if [ ! -f .env ]; then
    echo 'Create .env in the repository root before starting MemberOS.' >&2
    exit 2
fi

set -a
. ./.env
set +a

exec go run ./cmd/memberos "$@"
