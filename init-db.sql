-- ============================================================================
-- Beacon Stack — Postgres initialization
--
-- This script runs ONCE on the first start when the pgdata volume is empty.
-- It creates a dedicated database and user for each Beacon application.
-- Each app handles its own schema migrations via Goose on startup.
--
-- IMPORTANT: The passwords below must match the passwords in the DATABASE_DSN
-- environment variables in docker-compose.yml. If you change a password here,
-- update the corresponding DSN in docker-compose.yml before first run. After
-- first run, changing passwords requires dropping the pgdata volume and
-- re-initializing: docker compose down -v && docker compose up -d
-- ============================================================================

CREATE USER pulse WITH PASSWORD 'pulse';
CREATE USER pilot WITH PASSWORD 'pilot';
CREATE USER prism WITH PASSWORD 'prism';
CREATE USER haul  WITH PASSWORD 'haul';

CREATE DATABASE pulse_db OWNER pulse;
CREATE DATABASE pilot_db OWNER pilot;
CREATE DATABASE prism_db OWNER prism;
CREATE DATABASE haul_db  OWNER haul;

GRANT ALL PRIVILEGES ON DATABASE pulse_db TO pulse;
GRANT ALL PRIVILEGES ON DATABASE pilot_db TO pilot;
GRANT ALL PRIVILEGES ON DATABASE prism_db TO prism;
GRANT ALL PRIVILEGES ON DATABASE haul_db  TO haul;
