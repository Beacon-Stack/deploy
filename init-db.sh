#!/usr/bin/env bash
# ============================================================================
# Beacon Stack — Postgres initialization
#
# Runs ONCE on first start when the pgdata volume is empty. Creates a
# dedicated database + user for each Beacon application, pulling the
# per-app password from the shared beacon-secrets volume mounted at
# /run/secrets/<app>.txt. The init-secrets sidecar populates those files
# before Postgres starts.
#
# Passwords are passed into SQL via psql's :'var' substitution, which
# safely quotes and escapes the value — including passwords that contain
# single quotes or backslashes.
#
# Changing a password later requires dropping BOTH the pgdata and
# beacon-secrets volumes:
#   docker compose down -v
#   docker compose up -d
# ============================================================================

set -euo pipefail

secret() {
    local path="/run/secrets/$1.txt"
    if [[ ! -f "$path" ]]; then
        echo "init-db: missing secret file $path" >&2
        exit 1
    fi
    # Trim trailing whitespace to match what the apps do.
    tr -d '\r\n \t' < "$path"
}

PULSE_PW="$(secret pulse)"
PILOT_PW="$(secret pilot)"
PRISM_PW="$(secret prism)"
HAUL_PW="$(secret haul)"

psql -v ON_ERROR_STOP=1 \
    --username "$POSTGRES_USER" \
    -v pulse_pw="$PULSE_PW" \
    -v pilot_pw="$PILOT_PW" \
    -v prism_pw="$PRISM_PW" \
    -v haul_pw="$HAUL_PW" <<-'EOSQL'
    CREATE USER pulse WITH PASSWORD :'pulse_pw';
    CREATE USER pilot WITH PASSWORD :'pilot_pw';
    CREATE USER prism WITH PASSWORD :'prism_pw';
    CREATE USER haul  WITH PASSWORD :'haul_pw';

    CREATE DATABASE pulse_db OWNER pulse;
    CREATE DATABASE pilot_db OWNER pilot;
    CREATE DATABASE prism_db OWNER prism;
    CREATE DATABASE haul_db  OWNER haul;

    GRANT ALL PRIVILEGES ON DATABASE pulse_db TO pulse;
    GRANT ALL PRIVILEGES ON DATABASE pilot_db TO pilot;
    GRANT ALL PRIVILEGES ON DATABASE prism_db TO prism;
    GRANT ALL PRIVILEGES ON DATABASE haul_db  TO haul;
EOSQL
