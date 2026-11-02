#!/usr/bin/env bash
# Dumps the database to backups/ while the app keeps running. --single-transaction
# reads one consistent snapshot without locking the tables.
# The file only gets its final name once the dump has succeeded, so anything
# named *.sql.gz in backups/ is a whole dump.
set -euo pipefail

cd "$(dirname "$0")/.."
mkdir -p backups
file="backups/pastebin-$(date -u +%Y%m%dT%H%M%SZ).sql.gz"
trap 'rm -f "$file.tmp"' EXIT

docker compose exec -T mysql sh -c \
  'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysqldump -uroot --single-transaction --no-tablespaces --set-gtid-purged=OFF "$MYSQL_DATABASE"' \
  | gzip >"$file.tmp"
mv "$file.tmp" "$file"
echo "$file ($(du -h "$file" | cut -f1))"
