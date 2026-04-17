#!/usr/bin/env bash
# ============================================================================
# Beacon Stack — first-run secret setup
#
# Generates strong random passwords for Postgres and per-app database users.
# Creates placeholder VPN credential files (you must edit those by hand
# before running `docker compose up`).
#
# Idempotent: existing secret files are never overwritten.
# ============================================================================

set -euo pipefail

cd "$(dirname "$0")"

if ! command -v openssl >/dev/null 2>&1; then
    echo "setup-secrets: openssl is required but not installed." >&2
    exit 1
fi

randpw() {
    # 32 URL-safe-ish characters, no trailing newline. Avoids characters
    # that would need escaping in the DSN or psql.
    openssl rand -base64 32 | tr -d '/=+\n' | head -c 32
}

new_secret() {
    local name="$1"
    local generator="$2"
    local out="secrets/${name}.txt"
    if [[ -f "$out" ]]; then
        echo "skip:    $out (already exists)"
        return 0
    fi
    "$generator" > "$out"
    chmod 0600 "$out"
    echo "wrote:   $out"
}

placeholder_copy() {
    local name="$1"
    local out="secrets/${name}.txt"
    local src="secrets/${name}.txt.example"
    if [[ -f "$out" ]]; then
        echo "skip:    $out (already exists)"
        return 0
    fi
    cp "$src" "$out"
    chmod 0600 "$out"
    echo "placed:  $out  <-- EDIT BEFORE 'docker compose up'"
}

echo "Generating database passwords..."
new_secret pg-password randpw
for app in pulse pilot prism haul; do
    new_secret "${app}-db-password" randpw
done

echo ""
echo "Preparing VPN credential placeholders..."
placeholder_copy vpn-username
placeholder_copy vpn-password

echo ""
echo "Done. Next steps:"
echo "  1. If you're using the vpn profile, edit secrets/vpn-username.txt"
echo "     and secrets/vpn-password.txt with your VPN provider credentials."
echo "  2. cp .env.example .env and edit to taste."
echo "  3. docker compose up -d"
