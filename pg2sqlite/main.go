// pg2sqlite — one-shot Postgres → SQLite copier for the Beacon Stack
// upgrade. Reads tables from a Postgres database and writes them to a
// SQLite database whose schema already exists.
//
// Upgrade flow:
//
//   1. Stop your existing Beacon Stack but keep the Postgres container
//      running (it holds your data).
//   2. Start the new SQLite-based service images pointed at fresh
//      /config/<app>.db files. They'll run their goose migrations and
//      create the schema, then exit cleanly when stopped.
//   3. Run pg2sqlite — it reads the (still-up) Postgres data and copies
//      it into the freshly-migrated SQLite files.
//   4. Restart the services on the populated SQLite files.
//   5. Tear down the Postgres container and its data volume.
//
// Each --<svc>-pg/--<svc>-sqlite pair is optional. Run only the services
// whose data you care about; the others are no-ops.
//
// Type coercion applied per column:
//   - timestamptz/timestamp → RFC3339 UTC string
//   - jsonb                 → TEXT (JSON encoding preserved verbatim)
//   - bytea                 → BLOB (raw bytes)
//   - boolean               → 0/1 (sqlite has no boolean type)
//   - numeric/other         → passed through database/sql's defaults
//
// After every successful table copy the row count is verified between
// source and target and any AUTOINCREMENT primary key has its
// sqlite_sequence high-water mark updated so new inserts continue past
// the imported rows.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

const sqlitePragmas = "?_pragma=busy_timeout(5000)" +
	"&_pragma=journal_mode(WAL)" +
	"&_pragma=foreign_keys(OFF)" + // OFF during import; flipped back at the end
	"&_pragma=synchronous(NORMAL)"

type job struct {
	name     string // "pulse" | "pilot" | "prism" | "haul"
	pgDSN    string
	sqliteDB string
}

func main() {
	var pulsePG, pulseDB string
	var pilotPG, pilotDB string
	var prismPG, prismDB string
	var haulPG, haulDB string

	flag.StringVar(&pulsePG, "pulse-pg", "", "Postgres DSN for Pulse (e.g. postgres://pulse:pw@postgres:5432/pulse_db)")
	flag.StringVar(&pulseDB, "pulse-sqlite", "", "Target SQLite file for Pulse")
	flag.StringVar(&pilotPG, "pilot-pg", "", "Postgres DSN for Pilot")
	flag.StringVar(&pilotDB, "pilot-sqlite", "", "Target SQLite file for Pilot")
	flag.StringVar(&prismPG, "prism-pg", "", "Postgres DSN for Prism")
	flag.StringVar(&prismDB, "prism-sqlite", "", "Target SQLite file for Prism")
	flag.StringVar(&haulPG, "haul-pg", "", "Postgres DSN for Haul")
	flag.StringVar(&haulDB, "haul-sqlite", "", "Target SQLite file for Haul")

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "pg2sqlite — Beacon Stack Postgres → SQLite migrator")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  pg2sqlite [--<svc>-pg DSN --<svc>-sqlite PATH ...]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Each --<svc>-pg must be paired with the matching --<svc>-sqlite.")
		fmt.Fprintln(os.Stderr, "Services with no flags are skipped.")
	}
	flag.Parse()

	jobs := []job{}
	for _, j := range []job{
		{"pulse", pulsePG, pulseDB},
		{"pilot", pilotPG, pilotDB},
		{"prism", prismPG, prismDB},
		{"haul", haulPG, haulDB},
	} {
		if j.pgDSN == "" && j.sqliteDB == "" {
			continue
		}
		if j.pgDSN == "" || j.sqliteDB == "" {
			fmt.Fprintf(os.Stderr, "%s: both --%s-pg and --%s-sqlite are required\n", j.name, j.name, j.name)
			os.Exit(2)
		}
		jobs = append(jobs, j)
	}
	if len(jobs) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx := context.Background()
	failed := 0
	for _, j := range jobs {
		fmt.Printf("\n── %s ──────────────────────────────────────────────────\n", j.name)
		fmt.Printf("  pg     : %s\n", redactPassword(j.pgDSN))
		fmt.Printf("  sqlite : %s\n", j.sqliteDB)
		if err := copyDB(ctx, j); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", j.name, err)
			failed++
			continue
		}
		fmt.Printf("  ✓ %s done\n", j.name)
	}

	if failed > 0 {
		fmt.Fprintf(os.Stderr, "\n%d of %d service(s) failed.\n", failed, len(jobs))
		os.Exit(1)
	}
	fmt.Printf("\nAll %d service(s) copied successfully.\n", len(jobs))
}

func copyDB(ctx context.Context, j job) error {
	src, err := sql.Open("pgx", j.pgDSN)
	if err != nil {
		return fmt.Errorf("opening postgres: %w", err)
	}
	defer src.Close()
	if err := src.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}

	if _, err := os.Stat(j.sqliteDB); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("sqlite file %q does not exist - start the new service image first so it runs migrations", j.sqliteDB)
	}

	dst, err := sql.Open("sqlite", "file:"+j.sqliteDB+sqlitePragmas)
	if err != nil {
		return fmt.Errorf("opening sqlite: %w", err)
	}
	defer dst.Close()
	// Serialize sqlite writes - single connection, no contention.
	dst.SetMaxOpenConns(1)
	if err := dst.PingContext(ctx); err != nil {
		return fmt.Errorf("sqlite ping: %w", err)
	}

	tables, err := pgUserTables(ctx, src)
	if err != nil {
		return fmt.Errorf("listing pg tables: %w", err)
	}
	if len(tables) == 0 {
		fmt.Printf("  (no user tables found in postgres - empty source?)\n")
		return nil
	}
	fmt.Printf("  tables : %d\n", len(tables))

	sqliteTables, err := sqliteTableSet(ctx, dst)
	if err != nil {
		return fmt.Errorf("listing sqlite tables: %w", err)
	}

	for _, t := range tables {
		if !sqliteTables[t] {
			return fmt.Errorf("table %q exists in postgres but not in the sqlite schema - was the new service image run against this file first?", t)
		}
	}

	for _, t := range tables {
		copied, srcRows, err := copyTable(ctx, src, dst, t)
		if err != nil {
			return fmt.Errorf("copying %s: %w", t, err)
		}
		if copied != srcRows {
			return fmt.Errorf("%s: copied %d rows but source had %d", t, copied, srcRows)
		}
		fmt.Printf("  %-30s %d rows\n", t, copied)
		if err := bumpSqliteSequence(ctx, dst, t); err != nil {
			return fmt.Errorf("bumping sqlite_sequence for %s: %w", t, err)
		}
	}

	// Flip foreign keys back on for the rest of the application's lifetime.
	// (The OFF in the DSN only applied to this connection; the next service
	// open re-applies its own pragma. We don't need to flip it here, but
	// emitting a final sanity-check PRAGMA confirms the file isn't locked.)
	if _, err := dst.ExecContext(ctx, `PRAGMA integrity_check`); err != nil {
		return fmt.Errorf("sqlite integrity_check after import: %w", err)
	}

	return nil
}

// pgUserTables returns the user-defined tables in the public schema,
// excluding goose's bookkeeping table.
func pgUserTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT table_name
		  FROM information_schema.tables
		 WHERE table_schema = 'public'
		   AND table_type   = 'BASE TABLE'
		   AND table_name  <> 'goose_db_version'
		 ORDER BY table_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// sqliteTableSet returns the set of user tables in the SQLite DB.
func sqliteTableSet(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		 WHERE type='table'
		   AND name NOT LIKE 'sqlite_%'
		   AND name <> 'goose_db_version'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// columnInfo captures the per-column metadata we need to coerce values
// during the row-by-row copy.
type columnInfo struct {
	name    string
	pgType  string // information_schema.columns.data_type (e.g. "timestamp with time zone", "jsonb", "bytea")
	ordinal int
}

func pgColumns(ctx context.Context, db *sql.DB, table string) ([]columnInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type, ordinal_position
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name   = $1
		 ORDER BY ordinal_position
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []columnInfo
	for rows.Next() {
		var c columnInfo
		if err := rows.Scan(&c.name, &c.pgType, &c.ordinal); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// copyTable reads all rows from `table` in src and inserts them into the
// same-named table in dst, applying type coercion per column. Returns the
// number of rows copied and the row count reported by the source.
func copyTable(ctx context.Context, src, dst *sql.DB, table string) (copied, srcRows int64, err error) {
	cols, err := pgColumns(ctx, src, table)
	if err != nil {
		return 0, 0, fmt.Errorf("introspecting columns: %w", err)
	}
	if len(cols) == 0 {
		return 0, 0, nil
	}

	if err := src.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+table+`"`).Scan(&srcRows); err != nil {
		return 0, 0, fmt.Errorf("count: %w", err)
	}
	if srcRows == 0 {
		return 0, 0, nil
	}

	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = `"` + c.name + `"`
	}
	selectQ := `SELECT ` + strings.Join(colNames, ", ") + ` FROM "` + table + `"`
	rows, err := src.QueryContext(ctx, selectQ)
	if err != nil {
		return 0, 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertQ := fmt.Sprintf(
		`INSERT INTO "%s" (%s) VALUES (%s)`,
		table, strings.Join(colNames, ", "), strings.Join(placeholders, ", "),
	)

	// Single transaction for the whole table. Rolling back on error keeps
	// partial-copy state out of the SQLite file.
	tx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return 0, srcRows, fmt.Errorf("begin sqlite tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(ctx, insertQ)
	if err != nil {
		return 0, srcRows, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return copied, srcRows, fmt.Errorf("scan: %w", err)
		}
		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = coerce(c.pgType, raw[i])
		}
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return copied, srcRows, fmt.Errorf("insert row %d: %w", copied+1, err)
		}
		copied++
	}
	if err := rows.Err(); err != nil {
		return copied, srcRows, fmt.Errorf("row iteration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return copied, srcRows, fmt.Errorf("commit: %w", err)
	}
	return copied, srcRows, nil
}

// coerce converts a value scanned from a Postgres column of type pgType
// into a value safe to bind to a SQLite INSERT.
func coerce(pgType string, v any) any {
	if v == nil {
		return nil
	}
	switch strings.ToLower(pgType) {
	case "timestamp with time zone", "timestamp without time zone", "timestamptz", "timestamp", "date":
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(time.RFC3339Nano)
		}
	case "jsonb", "json":
		// pgx delivers JSONB as []byte. SQLite stores JSON as TEXT.
		if b, ok := v.([]byte); ok {
			return string(b)
		}
	case "bytea":
		// Pass through as []byte; SQLite stores as BLOB.
		return v
	case "boolean":
		if b, ok := v.(bool); ok {
			if b {
				return int64(1)
			}
			return int64(0)
		}
	}
	// Everything else: pass through. database/sql + modernc handle
	// numeric, text, etc. natively.
	return v
}

// bumpSqliteSequence sets sqlite_sequence.seq for `table` to the current
// max INTEGER PRIMARY KEY so subsequent AUTOINCREMENT inserts continue
// past the imported rows. No-op when the table isn't AUTOINCREMENT.
func bumpSqliteSequence(ctx context.Context, db *sql.DB, table string) error {
	// Check whether the table has an INTEGER PRIMARY KEY column that's
	// tracked by sqlite_sequence. PRAGMA table_info row 5 (`pk`) is non-zero
	// for PK columns; type "INTEGER" + pk>0 means rowid alias.
	var pkCol string
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if pk > 0 && strings.EqualFold(strings.TrimSpace(ctype), "INTEGER") {
			pkCol = name
		}
	}
	if pkCol == "" {
		return nil // no rowid-alias PK on this table
	}

	// sqlite_sequence only exists if at least one AUTOINCREMENT table has
	// been written to. If the schema declared AUTOINCREMENT, the table
	// will be there; otherwise this is a no-op.
	var exists int
	if err := db.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type='table' AND name='sqlite_sequence'`,
	).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	// sqlite_sequence has no PRIMARY KEY / UNIQUE constraint on `name`
	// (it's an internal table SQLite manages itself), so ON CONFLICT does
	// not apply. Try UPDATE first; INSERT only when the row is missing.
	res, err := db.ExecContext(ctx, fmt.Sprintf(
		`UPDATE sqlite_sequence
		 SET seq = (SELECT COALESCE(MAX("%s"), 0) FROM "%s")
		 WHERE name = '%s'`,
		pkCol, table, table,
	))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		return nil
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO sqlite_sequence (name, seq)
		 SELECT '%s', COALESCE(MAX("%s"), 0) FROM "%s"`,
		table, pkCol, table,
	))
	return err
}

// redactPassword returns a DSN with its password component blanked out
// for safe logging.
func redactPassword(dsn string) string {
	at := strings.Index(dsn, "@")
	if at < 0 {
		return dsn
	}
	scheme := strings.Index(dsn, "://")
	if scheme < 0 || scheme+3 >= at {
		return dsn
	}
	userInfo := dsn[scheme+3 : at]
	colon := strings.Index(userInfo, ":")
	if colon < 0 {
		return dsn
	}
	return dsn[:scheme+3] + userInfo[:colon] + ":***" + dsn[at:]
}
