#!/bin/sh
# Create per-service Postgres users and databases on first run.
# Idempotent: existing roles and databases are left alone.
# Run by the init-databases sidecar in docker-compose.yml.
set -eu

export PGPASSWORD="$(cat /run/secrets/pg.txt)"

for name in pulse pilot prism haul; do
  pw="$(cat /run/secrets/${name}.txt)"

  exists=$(psql -h postgres -U postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='${name}'")
  if [ "$exists" != "1" ]; then
    psql -h postgres -U postgres -v pw="$pw" -c "CREATE USER ${name} WITH PASSWORD :'pw'"
  fi

  dbexists=$(psql -h postgres -U postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${name}_db'")
  if [ "$dbexists" != "1" ]; then
    psql -h postgres -U postgres -c "CREATE DATABASE ${name}_db OWNER ${name}"
  fi
done
