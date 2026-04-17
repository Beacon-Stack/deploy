#!/bin/sh
# ============================================================================
# Beacon Stack — first-run secret generator
#
# Runs inside the 'init-secrets' one-shot container on every `docker compose
# up`. On first run, fills the shared 'beacon-secrets' volume with one random
# password per service (pg + pulse + pilot + prism + haul). On subsequent
# runs, every file already exists and the script is a no-op.
#
# Passwords never leave the Docker-managed volume. To rotate, stop the stack,
# `docker volume rm beacon-secrets`, then `docker compose up -d` — but note
# this also requires dropping the pgdata volume (Postgres has the old
# password hashes baked in).
# ============================================================================

set -eu

for name in pg pulse pilot prism haul; do
    target="/secrets/${name}.txt"
    if [ ! -s "${target}" ]; then
        head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 32 > "${target}"
        chmod 0600 "${target}"
        echo "generated ${target}"
    else
        echo "skip ${target} (already exists)"
    fi
done
