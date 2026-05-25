# pg2sqlite

One-shot Postgres → SQLite copier for the Beacon Stack upgrade. See the main [deploy README](../README.md#upgrading-from-postgres) for the end-user workflow.

## Building

```bash
go build .
./pg2sqlite --help
```

## Testing

Unit tests (no docker required):

```bash
go test ./...
```

Integration test (requires docker, host port 5433 free):

```bash
go test -tags=integration -timeout 2m ./...
```

The integration test:
- Spins up `postgres:17-alpine` in docker
- Creates a schema covering every type pg2sqlite coerces (TEXT, BOOLEAN, INTEGER, BIGINT, REAL, JSONB, BYTEA, TIMESTAMPTZ, BIGSERIAL, nullable columns)
- Seeds 3 rows with NULL/non-NULL variations
- Builds pg2sqlite from current source
- Runs it as a subprocess against the live Postgres + a fresh SQLite file with matching schema
- Verifies row counts and column-by-column content (including JSON structural equality, byte-blob equality, RFC3339 timestamp round-trip)
- Inserts a row after the copy to confirm `sqlite_sequence` was bumped and AUTOINCREMENT continues past imported BIGSERIAL ids

The test container is named `pg2sqlite-integration-test`; if a previous run left it behind, the next run cleans it up first.
