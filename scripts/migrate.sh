#!/usr/bin/env bash
# Applies db/migrations/*.sql in filename order, each one exactly once.
# Every applied file is recorded in the schema_migrations table, so running
# this again only applies new files. Each file runs in its own transaction:
# if it fails, nothing from that file is kept and the script stops.
set -euo pipefail

MIGRATIONS_DIR="db/migrations"

# Run psql inside the postgres container, using the user and database
# the container was created with (from docker-compose.yml / .env).
psql_db() {
  docker compose exec -T postgres \
    sh -c 'psql -X -q -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" "$@"' psql "$@"
}

# 1. Make sure the bookkeeping table exists.
psql_db <<'SQL'
SET client_min_messages = warning;
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    text        PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);
SQL

# 2. Collect migration files (the shell sorts them by name).
shopt -s nullglob
files=("$MIGRATIONS_DIR"/*.sql)
if [ ${#files[@]} -eq 0 ]; then
  echo "No migrations found in $MIGRATIONS_DIR"
  exit 0
fi

# 3. Apply each file that is not recorded yet.
applied=0
for file in "${files[@]}"; do
  version="$(basename "$file" .sql)"

  if [[ ! "$version" =~ ^[0-9]{4}_[a-z0-9_]+$ ]]; then
    echo "Bad migration file name: $file (expected e.g. 0002_add_motifs.sql)" >&2
    exit 1
  fi

  if [ -n "$(psql_db -tA -c "SELECT 1 FROM schema_migrations WHERE version = '$version'")" ]; then
    echo "skip   $version (already applied)"
    continue
  fi

  echo "apply  $version"
  {
    echo "BEGIN;"
    cat "$file"
    echo
    echo "INSERT INTO schema_migrations (version) VALUES ('$version');"
    echo "COMMIT;"
  } | psql_db
  applied=$((applied + 1))
done

echo "Done: $applied new migration(s) applied."