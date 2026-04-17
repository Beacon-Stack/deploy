#!/usr/bin/env bash
# ============================================================================
# Beacon Stack — Postgres initialization
#
# Runs ONCE on first start when the pgdata volume is empty. Creates a
# dedicated database + user for each Beacon application, pulling the
# per-app password from the corresponding Docker secret file.
#
# Passwords are passed into SQL via psql's :'var' substitution, which
# safely quotes and escapes the value — including passwords that contain
# single quotes or backslashes.
#
# Changing a password later requires dropping the pgdata volume:
#   docker compose down -v && docker compose up -d
# ============================================================================

set -euo pipefail

secret() {
    local path="/run/secrets/$1"
    if [[ ! -f "$path" ]]; then
        echo "init-db: missing secret file $path" >&2
        exit 1
    fi
    # Trim trailing whitespace the same way the apps do, so the password
    # written into Postgres matches what the apps will send.
    tr -d '\r\n \t' < "$path"
}

PULSE_PW="$(secret pulse-db-password)"
PILOT_PW="$(secret pilot-db-password)"
PRISM_PW="$(secret prism-db-password)"
HAUL_PW="$(secret haul-db-password)"

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
