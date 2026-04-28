#!/bin/sh
# Generate random database passwords on first run.
# Idempotent: skips files that already exist.
# Run by the init-secrets sidecar in docker-compose.yml.
set -eu

for name in pg pulse pilot prism haul pulse-api-key; do
  f="/secrets/${name}.txt"
  if [ ! -s "$f" ]; then
    head -c 32 /dev/urandom | base64 | tr -d '/+=' | head -c 32 > "$f"
    echo "generated $f"
  fi
  chmod 0444 "$f"
done
