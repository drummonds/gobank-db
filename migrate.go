package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Migration is one versioned step in a schema's history.
//
// Versions must be positive, unique and strictly increasing within the list
// handed to Apply. Phase decides what the SQL may contain (see Lint). SQL is
// a script of one or more statements, split with SplitStatements and run
// inside a single transaction together with the schema_migrations record.
type Migration struct {
	Version int
	Name    string
	Phase   Phase
	SQL     string
}

// MigrationsTable is the name of the table Apply uses to record which
// versions have been applied.
const MigrationsTable = "schema_migrations"

const migrationsDDL = `CREATE TABLE IF NOT EXISTS ` + MigrationsTable + ` (
	version    INTEGER PRIMARY KEY,
	name       VARCHAR(200) NOT NULL,
	phase      VARCHAR(20) NOT NULL,
	applied_at TIMESTAMP NOT NULL
)`

// Apply brings the database up to date with migrations.
//
// Before touching the database it validates the whole list: versions in
// order, and every Expand and Cutover migration free of destructive DDL
// (Lint). It then creates schema_migrations if needed and runs, in order,
// each migration whose version is not yet recorded. Each migration runs in
// its own transaction with its schema_migrations row, so a failure leaves
// earlier migrations applied and the failing one fully rolled back
// (transactional DDL holds on both Postgres and pglike).
//
// Apply returns the versions it applied in this call. Running it again with
// the same list applies nothing and returns nil, nil.
func Apply(ctx context.Context, d *sql.DB, migrations []Migration) (applied []int, err error) {
	if err := Validate(migrations); err != nil {
		return nil, err
	}
	if _, err := d.ExecContext(ctx, migrationsDDL); err != nil {
		return nil, fmt.Errorf("db.Apply: create %s: %w", MigrationsTable, err)
	}
	done, err := AppliedVersions(ctx, d)
	if err != nil {
		return nil, err
	}
	for _, m := range migrations {
		if done[m.Version] {
			continue
		}
		if err := applyOne(ctx, d, m); err != nil {
			return applied, err
		}
		applied = append(applied, m.Version)
	}
	return applied, nil
}

func applyOne(ctx context.Context, d *sql.DB, m Migration) (err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db.Apply v%d %s: begin: %w", m.Version, m.Name, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, stmt := range SplitStatements(m.SQL) {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db.Apply v%d %s: %w\n  in: %s", m.Version, m.Name, err, oneLine(stmt, 120))
		}
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO `+MigrationsTable+` (version, name, phase, applied_at) VALUES ($1, $2, $3, $4)`,
		m.Version, m.Name, m.Phase.String(), time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("db.Apply v%d %s: record: %w", m.Version, m.Name, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("db.Apply v%d %s: commit: %w", m.Version, m.Name, err)
	}
	return nil
}

// AppliedVersions returns the set of migration versions recorded in
// schema_migrations. A database that has never been migrated yields an
// empty set, not an error.
func AppliedVersions(ctx context.Context, d *sql.DB) (map[int]bool, error) {
	done := map[int]bool{}
	rows, err := d.QueryContext(ctx, `SELECT version FROM `+MigrationsTable)
	if err != nil {
		if !tableExists(ctx, d, MigrationsTable) {
			return done, nil
		}
		return nil, fmt.Errorf("db.AppliedVersions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("db.AppliedVersions: %w", err)
		}
		done[v] = true
	}
	return done, rows.Err()
}

func tableExists(ctx context.Context, d *sql.DB, name string) bool {
	var n int
	err := d.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.tables WHERE table_name = $1`, name,
	).Scan(&n)
	return err == nil && n > 0
}

// Validate checks a migration list without touching a database: versions
// positive, unique and strictly increasing, names present, phases known,
// and each script clean under Lint for its phase.
func Validate(migrations []Migration) error {
	if len(migrations) == 0 {
		return errors.New("db.Validate: no migrations")
	}
	prev := 0
	for _, m := range migrations {
		if m.Version <= prev {
			return fmt.Errorf("db.Validate: v%d %q: versions must be positive and strictly increasing (previous v%d)", m.Version, m.Name, prev)
		}
		prev = m.Version
		if m.Name == "" {
			return fmt.Errorf("db.Validate: v%d: missing name", m.Version)
		}
		switch m.Phase {
		case Expand, Cutover, Contract:
		default:
			return fmt.Errorf("db.Validate: v%d %q: unknown phase %v", m.Version, m.Name, m.Phase)
		}
		if err := Lint(m.Phase, m.SQL); err != nil {
			return fmt.Errorf("db.Validate: v%d %q: %w", m.Version, m.Name, err)
		}
	}
	return nil
}
