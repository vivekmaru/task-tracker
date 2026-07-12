#!/usr/bin/env bash
set -euo pipefail

# Run only against a disposable database URL. This script never performs a
# downgrade; it demonstrates backup/restore compatibility with forward schema.
: "${FORGE_RECOVERY_DATABASE_URL:?set a disposable PostgreSQL URL}"
if [[ "${FORGE_RECOVERY_DATABASE_URL}" != *"forge_test"* && "${FORGE_RECOVERY_DATABASE_URL}" != *"forge_recovery"* ]]; then
  echo "refusing non-disposable recovery database URL" >&2
  exit 2
fi
database_path="${FORGE_RECOVERY_DATABASE_URL%%\?*}"
database_name="${database_path##*/}"
if [[ ! "${database_name}" =~ ^[a-zA-Z0-9_]+$ ]]; then
  echo "refusing unsafe database name" >&2
  exit 2
fi
maintenance_url="${database_path%/*}/postgres"
if [[ "${FORGE_RECOVERY_DATABASE_URL}" == *\?* ]]; then
  maintenance_url+="?${FORGE_RECOVERY_DATABASE_URL#*\?}"
fi
if ! psql "${maintenance_url}" -Atqc "SELECT 1 FROM pg_database WHERE datname = '${database_name}'" | grep -q 1; then
  psql "${maintenance_url}" -c "CREATE DATABASE \"${database_name}\""
fi

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT
go build -o "${workdir}/forge" ./cmd/forge
FORGE_DATABASE_URL="${FORGE_RECOVERY_DATABASE_URL}" "${workdir}/forge" migrate
if [[ -n "${FORGE_RECOVERY_PG_CONTAINER:-}" ]]; then
  # Use a client matching the target server major version. This is especially
  # useful for a disposable Docker PostgreSQL harness.
  docker exec "${FORGE_RECOVERY_PG_CONTAINER}" pg_dump -U postgres --format=custom --file /tmp/forge-recovery.dump "${database_name}"
  docker exec "${FORGE_RECOVERY_PG_CONTAINER}" pg_restore -U postgres --clean --if-exists --dbname "${database_name}" /tmp/forge-recovery.dump
else
  pg_dump --format=custom --file "${workdir}/forge.dump" "${FORGE_RECOVERY_DATABASE_URL}"
  pg_restore --clean --if-exists --dbname "${FORGE_RECOVERY_DATABASE_URL}" "${workdir}/forge.dump"
fi
FORGE_DATABASE_URL="${FORGE_RECOVERY_DATABASE_URL}" "${workdir}/forge" migrate
echo "recovery smoke passed"
