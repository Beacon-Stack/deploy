//go:build integration

// Integration test for pg2sqlite. Spins up a real Postgres container,
// applies a schema covering every type pg2sqlite needs to coerce, seeds
// representative rows, invokes the built binary, and verifies the
// resulting SQLite file row-by-row.
//
// Run with: go test -tags=integration ./...
// Requires: docker CLI available, port 5433 free.

package main

import (
	"database/sql"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

const (
	pgContainerName = "pg2sqlite-integration-test"
	pgHostPort      = "5433"
	pgDSN           = "postgres://postgres:test@127.0.0.1:5433/postgres?sslmode=disable"
)

// pgDDL exercises every type pg2sqlite is expected to coerce:
//
//   - TEXT, INTEGER, BIGINT, REAL  → passthrough
//   - BOOLEAN                      → 0/1
//   - TIMESTAMPTZ                  → RFC3339 string
//   - JSONB                        → JSON-encoded TEXT
//   - BYTEA                        → BLOB
//   - BIGSERIAL                    → INTEGER PRIMARY KEY AUTOINCREMENT
//   - Nullable columns             → nil passthrough
const pgDDL = `
CREATE TABLE items (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    count         INTEGER NOT NULL DEFAULT 0,
    big_count     BIGINT NOT NULL DEFAULT 0,
    score         REAL,
    payload       JSONB NOT NULL DEFAULT '{}',
    binary_blob   BYTEA,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    nullable_text TEXT
);

CREATE TABLE counters (
    id    BIGSERIAL PRIMARY KEY,
    label TEXT NOT NULL
);

CREATE INDEX items_name_idx ON items (name);
`

// sqliteDDL is the SQLite-flavoured equivalent that pg2sqlite expects to find
// already in place (created by booting the new service image with
// migrations once before the copy step).
const sqliteDDL = `
CREATE TABLE items (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    count         INTEGER NOT NULL DEFAULT 0,
    big_count     BIGINT NOT NULL DEFAULT 0,
    score         REAL,
    payload       TEXT NOT NULL DEFAULT '{}',
    binary_blob   BLOB,
    created_at    TEXT NOT NULL,
    nullable_text TEXT
);

CREATE TABLE counters (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    label TEXT NOT NULL
);

CREATE INDEX items_name_idx ON items (name);
`

func TestIntegration_FullCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires docker; -short")
	}
	startPostgres(t)
	pg := waitForPostgres(t, 60*time.Second)
	defer pg.Close()

	if _, err := pg.Exec(pgDDL); err != nil {
		t.Fatalf("applying pg schema: %v", err)
	}
	seed := seedRows(t, pg)

	// Build pg2sqlite from the current source so the test can't drift.
	bin := filepath.Join(t.TempDir(), "pg2sqlite")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building pg2sqlite: %v\n%s", err, out)
	}

	sqlitePath := filepath.Join(t.TempDir(), "target.db")
	createSqliteWithSchema(t, sqlitePath)

	out, err := exec.Command(bin,
		"--pulse-pg", pgDSN,
		"--pulse-sqlite", sqlitePath,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("pg2sqlite failed: %v\noutput:\n%s", err, out)
	}
	t.Logf("pg2sqlite output:\n%s", out)

	sq, err := sql.Open("sqlite", "file:"+sqlitePath+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	defer sq.Close()

	verifyItemsTable(t, sq, seed)
	verifyCountersTable(t, sq)
	verifyAutoincrementContinues(t, sq)
}

// ── helpers ────────────────────────────────────────────────────────────────

func startPostgres(t *testing.T) {
	t.Helper()
	// Tear down any leftover container from a previous failed run.
	_ = exec.Command("docker", "rm", "-f", pgContainerName).Run()
	out, err := exec.Command("docker", "run", "-d", "--rm",
		"--name", pgContainerName,
		"-e", "POSTGRES_PASSWORD=test",
		"-p", pgHostPort+":5432",
		"postgres:17-alpine",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run postgres: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", pgContainerName).Run()
	})
}

// waitForPostgres polls Open/Ping until the container is ready or timeout.
func waitForPostgres(t *testing.T, timeout time.Duration) *sql.DB {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := sql.Open("pgx", pgDSN)
		if err == nil {
			if err = db.Ping(); err == nil {
				return db
			}
			db.Close()
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres never ready: %v", lastErr)
	return nil
}

type seededRow struct {
	id           string
	name         string
	enabled      bool
	count        int32
	bigCount     int64
	score        *float64
	payload      string
	binary       []byte
	createdAt    time.Time
	nullableText *string
}

func seedRows(t *testing.T, pg *sql.DB) []seededRow {
	t.Helper()
	// 4.5 is exactly representable as a 32-bit float; arbitrary decimals
	// like 4.2 lose precision because Postgres REAL is single-precision.
	score := 4.5
	nullStr := "present"
	rows := []seededRow{
		{
			id:           "row-a",
			name:         "alpha",
			enabled:      true,
			count:        1,
			bigCount:     10_000_000_000,
			score:        &score,
			payload:      `{"k":"v","n":42}`,
			binary:       []byte{0xde, 0xad, 0xbe, 0xef},
			createdAt:    time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
			nullableText: &nullStr,
		},
		{
			id:           "row-b",
			name:         "beta",
			enabled:      false,
			count:        0,
			bigCount:     0,
			score:        nil, // NULL
			payload:      `[]`,
			binary:       nil, // NULL
			createdAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			nullableText: nil, // NULL
		},
		{
			id:           "row-c",
			name:         "gamma",
			enabled:      true,
			count:        -7,
			bigCount:     -1,
			score:        &score,
			payload:      `{"emoji":"✨"}`,
			binary:       []byte{0x00, 0x01, 0x02},
			createdAt:    time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC),
			nullableText: nil,
		},
	}
	for _, r := range rows {
		_, err := pg.Exec(`
			INSERT INTO items
			  (id, name, enabled, count, big_count, score, payload, binary_blob, created_at, nullable_text)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
		`, r.id, r.name, r.enabled, r.count, r.bigCount, r.score, r.payload, r.binary, r.createdAt, r.nullableText)
		if err != nil {
			t.Fatalf("seeding items %s: %v", r.id, err)
		}
	}
	// Counters: insert 3 rows with auto-assigned BIGSERIAL ids. After copy,
	// SQLite's sqlite_sequence must be bumped to 3 so the next insert is 4.
	for _, label := range []string{"first", "second", "third"} {
		if _, err := pg.Exec(`INSERT INTO counters (label) VALUES ($1)`, label); err != nil {
			t.Fatalf("seeding counters: %v", err)
		}
	}
	return rows
}

func createSqliteWithSchema(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("opening fresh sqlite: %v", err)
	}
	defer db.Close()
	for _, stmt := range strings.Split(sqliteDDL, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("apply sqlite schema (%q): %v", stmt, err)
		}
	}
}

func verifyItemsTable(t *testing.T, sq *sql.DB, want []seededRow) {
	t.Helper()
	var got int
	if err := sq.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&got); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if got != len(want) {
		t.Fatalf("items count = %d; want %d", got, len(want))
	}

	for _, w := range want {
		var (
			id, name, payload, createdAt string
			enabled                      bool
			count                        int64
			bigCount                     int64
			score                        sql.NullFloat64
			binary                       []byte
			nullableText                 sql.NullString
		)
		err := sq.QueryRow(`
			SELECT id, name, enabled, count, big_count, score, payload, binary_blob, created_at, nullable_text
			  FROM items WHERE id = ?
		`, w.id).Scan(&id, &name, &enabled, &count, &bigCount, &score, &payload, &binary, &createdAt, &nullableText)
		if err != nil {
			t.Errorf("%s: scan: %v", w.id, err)
			continue
		}

		if name != w.name {
			t.Errorf("%s: name = %q; want %q", w.id, name, w.name)
		}
		if enabled != w.enabled {
			t.Errorf("%s: enabled = %v; want %v", w.id, enabled, w.enabled)
		}
		if count != int64(w.count) {
			t.Errorf("%s: count = %d; want %d", w.id, count, w.count)
		}
		if bigCount != w.bigCount {
			t.Errorf("%s: big_count = %d; want %d", w.id, bigCount, w.bigCount)
		}
		switch {
		case w.score == nil && score.Valid:
			t.Errorf("%s: score = %v; want NULL", w.id, score.Float64)
		case w.score != nil && !score.Valid:
			t.Errorf("%s: score = NULL; want %v", w.id, *w.score)
		case w.score != nil && score.Float64 != *w.score:
			t.Errorf("%s: score = %v; want %v", w.id, score.Float64, *w.score)
		}

		// payload: should be the same JSON text. Compare structurally to
		// tolerate whitespace/key-order differences from pg's jsonb canonicalisation.
		var wantPL, gotPL any
		if err := json.Unmarshal([]byte(w.payload), &wantPL); err != nil {
			t.Errorf("%s: bad want payload: %v", w.id, err)
		}
		if err := json.Unmarshal([]byte(payload), &gotPL); err != nil {
			t.Errorf("%s: payload not valid JSON in sqlite (%q): %v", w.id, payload, err)
		} else if !jsonEqual(wantPL, gotPL) {
			t.Errorf("%s: payload mismatch\n got: %v\nwant: %v", w.id, gotPL, wantPL)
		}

		if w.binary == nil && binary != nil {
			t.Errorf("%s: binary_blob = %x; want NULL", w.id, binary)
		}
		if w.binary != nil && !bytesEqual(binary, w.binary) {
			t.Errorf("%s: binary_blob = %x; want %x", w.id, binary, w.binary)
		}

		// created_at must be a parseable RFC3339 timestamp matching the
		// original instant (UTC).
		ts, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			t.Errorf("%s: created_at not RFC3339 (%q): %v", w.id, createdAt, err)
		} else if !ts.UTC().Equal(w.createdAt.UTC()) {
			t.Errorf("%s: created_at = %s; want %s", w.id, ts.UTC(), w.createdAt.UTC())
		}

		switch {
		case w.nullableText == nil && nullableText.Valid:
			t.Errorf("%s: nullable_text = %q; want NULL", w.id, nullableText.String)
		case w.nullableText != nil && !nullableText.Valid:
			t.Errorf("%s: nullable_text = NULL; want %q", w.id, *w.nullableText)
		case w.nullableText != nil && nullableText.String != *w.nullableText:
			t.Errorf("%s: nullable_text = %q; want %q", w.id, nullableText.String, *w.nullableText)
		}
	}
}

func verifyCountersTable(t *testing.T, sq *sql.DB) {
	t.Helper()
	rows, err := sq.Query(`SELECT id, label FROM counters ORDER BY id`)
	if err != nil {
		t.Fatalf("query counters: %v", err)
	}
	defer rows.Close()
	wantLabels := []string{"first", "second", "third"}
	i := 0
	for rows.Next() {
		var id int64
		var label string
		if err := rows.Scan(&id, &label); err != nil {
			t.Fatalf("scan counters: %v", err)
		}
		if i >= len(wantLabels) {
			t.Errorf("extra counters row: id=%d label=%q", id, label)
			break
		}
		// BIGSERIAL ids in Postgres start at 1; the copy must preserve them.
		if id != int64(i+1) {
			t.Errorf("counters[%d].id = %d; want %d (BIGSERIAL ids must round-trip)", i, id, i+1)
		}
		if label != wantLabels[i] {
			t.Errorf("counters[%d].label = %q; want %q", i, label, wantLabels[i])
		}
		i++
	}
	if i != len(wantLabels) {
		t.Errorf("counters row count = %d; want %d", i, len(wantLabels))
	}
}

// verifyAutoincrementContinues inserts a new counters row after the copy
// and confirms its assigned id is MAX(existing)+1 rather than restarting.
// This is the sqlite_sequence high-water-mark bump.
func verifyAutoincrementContinues(t *testing.T, sq *sql.DB) {
	t.Helper()
	res, err := sq.Exec(`INSERT INTO counters (label) VALUES (?)`, "fourth")
	if err != nil {
		t.Fatalf("post-copy insert: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if id != 4 {
		t.Errorf("post-copy AUTOINCREMENT id = %d; want 4 (sqlite_sequence high-water mark must be bumped)", id)
	}
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

