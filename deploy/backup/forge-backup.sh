#!/usr/bin/env bash
#
# Forge backup: a custom-format pg_dump of the database plus a tar of the local
# artifact root, with day-based retention. Restore drill lives in
# scripts/recovery-smoke.sh (see docs/release-and-recovery.md).
#
# Configure via environment (an EnvironmentFile works well under systemd):
#   FORGE_DATABASE_URL   Postgres connection string (required; never printed)
#   FORGE_ARTIFACT_ROOT  Local artifact directory to archive (optional)
#   FORGE_BACKUP_DIR     Destination directory (default /var/backups/forge)
#   FORGE_BACKUP_RETENTION_DAYS  Delete backups older than this (default 14)
set -euo pipefail

: "${FORGE_DATABASE_URL:?FORGE_DATABASE_URL is required}"
backup_dir="${FORGE_BACKUP_DIR:-/var/backups/forge}"
retention_days="${FORGE_BACKUP_RETENTION_DAYS:-14}"
artifact_root="${FORGE_ARTIFACT_ROOT:-}"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$backup_dir"

db_dump="${backup_dir}/forge-db-${timestamp}.dump"
echo "Backing up database to ${db_dump}"
# --dbname consumes the URL without exposing it on the process argument list
# beyond this invocation; the value itself is never echoed.
pg_dump --dbname="$FORGE_DATABASE_URL" --format=custom --file="$db_dump"

if [ -n "$artifact_root" ] && [ -d "$artifact_root" ]; then
	artifact_tar="${backup_dir}/forge-artifacts-${timestamp}.tar.gz"
	echo "Archiving artifact root ${artifact_root} to ${artifact_tar}"
	tar -czf "$artifact_tar" -C "$artifact_root" .
else
	echo "No artifact root to archive (FORGE_ARTIFACT_ROOT unset or missing); skipping"
fi

echo "Pruning backups older than ${retention_days} days"
find "$backup_dir" -maxdepth 1 -type f -name 'forge-db-*.dump' -mtime "+${retention_days}" -delete
find "$backup_dir" -maxdepth 1 -type f -name 'forge-artifacts-*.tar.gz' -mtime "+${retention_days}" -delete

echo "Backup complete"
