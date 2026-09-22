package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// The migration history used by these tests walks the full
// expand → cutover → contract cycle around a contract view.
//
// Services never name a physical table: they read `accounts`, which is a
// view. The physical table can therefore be rebuilt underneath (blue/green
// in one database) and the view repointed in a single transaction.
var testMigrations = []Migration{
	{1, "accounts table and contract view", Expand, `
		CREATE TABLE accounts_blue (
			id       VARCHAR(36) PRIMARY KEY,
			name     VARCHAR(100) NOT NULL,
			currency VARCHAR(3) NOT NULL DEFAULT 'GBP',
			balance  BIGINT NOT NULL DEFAULT 0
		);
		CREATE VIEW accounts AS
			SELECT id, name, currency, balance FROM accounts_blue;
	`},
	{2, "add opened_on", Expand, `
		ALTER TABLE accounts_blue ADD COLUMN opened_on DATE;
	`},
	{3, "green copy with opened_on backfilled", Expand, `
		CREATE TABLE accounts_green (
			id        VARCHAR(36) PRIMARY KEY,
			name      VARCHAR(100) NOT NULL,
			currency  VARCHAR(3) NOT NULL DEFAULT 'GBP',
			balance   BIGINT NOT NULL DEFAULT 0,
			opened_on DATE NOT NULL DEFAULT '2000-01-01'
		);
		INSERT INTO accounts_green (id, name, currency, balance, opened_on)
			SELECT id, name, currency, balance, COALESCE(opened_on, '2000-01-01')
			FROM accounts_blue;
	`},
	{4, "flip accounts view to green", Cutover, `
		DROP VIEW accounts;
		CREATE VIEW accounts AS
			SELECT id, name, currency, balance, opened_on FROM accounts_green;
	`},
	{5, "retire blue", Contract, `
		DROP TABLE accounts_blue;
	`},
}

func cleanupMigrationTables(t *testing.T, d *sql.DB) {
	t.Helper()
	for _, s := range []string{
		"DROP VIEW IF EXISTS accounts",
		"DROP TABLE IF EXISTS accounts_blue",
		"DROP TABLE IF EXISTS accounts_green",
		"DROP TABLE IF EXISTS " + MigrationsTable,
	} {
		d.Exec(s)
	}
}

func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d := openTestDB(t)
	if os.Getenv("GOBANK_TEST_DSN") != "" {
		cleanupMigrationTables(t, d)
		t.Cleanup(func() { cleanupMigrationTables(t, d) })
	}
	return d
}

// viewColumns reads the column list of a view with SELECT *.
//
// On Postgres via pgx this is exactly the query a contract-view cutover
// breaks: pgx caches the prepared statement per connection, and once the
// view's column set changes the cached plan fails with SQLSTATE 0A000
// ("cached plan must not change result type"). pgx evicts the entry on that
// error, so a single retry succeeds. Consumers that name their columns never
// see it, which is why the contract rule is "no SELECT * through a view".
func viewColumns(t *testing.T, d *sql.DB, view string) []string {
	t.Helper()
	q := "SELECT * FROM " + view + " WHERE 1=0"
	rows, err := d.Query(q)
	if err != nil && strings.Contains(err.Error(), "0A000") {
		t.Logf("%s: %v (retrying once, as a consumer must after a cutover)", q, err)
		rows, err = d.Query(q)
	}
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	return cols
}

func TestApplyFullCycle(t *testing.T) {
	d := openMigrationTestDB(t)
	ctx := context.Background()

	// Expand only: v1 and v2.
	applied, err := Apply(ctx, d, testMigrations[:2])
	if err != nil {
		t.Fatalf("Apply v1-2: %v", err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied %v, want %v", applied, want)
	}
	if _, err := d.ExecContext(ctx,
		"INSERT INTO accounts_blue (id, name, balance) VALUES ('a1', 'Cash', 100)"); err != nil {
		t.Fatal(err)
	}

	// Old contract still holds after the additive change: same columns.
	if got, want := viewColumns(t, d, "accounts"), []string{"id", "name", "currency", "balance"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("view columns after expand: %v, want %v", got, want)
	}

	// Re-running is a no-op.
	applied, err = Apply(ctx, d, testMigrations[:2])
	if err != nil || applied != nil {
		t.Fatalf("re-Apply: applied=%v err=%v, want nil,nil", applied, err)
	}

	// Build green and cut over. The read through the view before and after
	// returns the same row; only the shape changes.
	var before int64
	if err := d.QueryRowContext(ctx, "SELECT balance FROM accounts WHERE id = 'a1'").Scan(&before); err != nil {
		t.Fatalf("read before cutover: %v", err)
	}
	applied, err = Apply(ctx, d, testMigrations[:4])
	if err != nil {
		t.Fatalf("Apply v3-4: %v", err)
	}
	if want := []int{3, 4}; !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied %v, want %v", applied, want)
	}
	var after int64
	var opened string
	if err := d.QueryRowContext(ctx, "SELECT balance, opened_on FROM accounts WHERE id = 'a1'").Scan(&after, &opened); err != nil {
		t.Fatalf("read after cutover: %v", err)
	}
	if before != after || !strings.HasPrefix(opened, "2000-01-01") {
		t.Fatalf("after cutover: balance %d (before %d), opened_on %q", after, before, opened)
	}
	if got, want := viewColumns(t, d, "accounts"), []string{"id", "name", "currency", "balance", "opened_on"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("view columns after cutover: %v, want %v", got, want)
	}

	// Contract.
	if _, err := Apply(ctx, d, testMigrations); err != nil {
		t.Fatalf("Apply v5: %v", err)
	}
	if tableExists(ctx, d, "accounts_blue") {
		t.Fatal("accounts_blue still exists after contract")
	}
	done, err := AppliedVersions(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 5 {
		t.Fatalf("recorded versions %v, want 1..5", done)
	}

	var phase string
	if err := d.QueryRowContext(ctx, "SELECT phase FROM "+MigrationsTable+" WHERE version = 4").Scan(&phase); err != nil || phase != "cutover" {
		t.Fatalf("phase of v4 = %q, %v; want cutover", phase, err)
	}
}

func TestApplyRejectsDestructiveExpandBeforeRunningAnything(t *testing.T) {
	d := openMigrationTestDB(t)
	ctx := context.Background()

	bad := []Migration{
		{1, "ok", Expand, "CREATE TABLE accounts_blue (id INT)"},
		{2, "not ok", Expand, "ALTER TABLE accounts_blue DROP COLUMN id"},
	}
	applied, err := Apply(ctx, d, bad)
	var le *LintError
	if !errors.As(err, &le) || le.Operation != "DROP" {
		t.Fatalf("want LintError DROP, got applied=%v err=%v", applied, err)
	}
	if tableExists(ctx, d, "accounts_blue") || tableExists(ctx, d, MigrationsTable) {
		t.Fatal("lint failure must not touch the database")
	}
}

func TestApplyRollsBackFailedMigration(t *testing.T) {
	d := openMigrationTestDB(t)
	ctx := context.Background()

	ms := []Migration{
		{1, "ok", Expand, "CREATE TABLE accounts_blue (id INT)"},
		{2, "fails midway", Expand, `
			CREATE TABLE accounts_green (id INT);
			INSERT INTO no_such_table VALUES (1);
		`},
	}
	applied, err := Apply(ctx, d, ms)
	if err == nil {
		t.Fatal("want error from v2")
	}
	if !reflect.DeepEqual(applied, []int{1}) {
		t.Fatalf("applied %v, want [1]", applied)
	}
	if tableExists(ctx, d, "accounts_green") {
		t.Fatal("accounts_green should have been rolled back")
	}
	done, _ := AppliedVersions(ctx, d)
	if !done[1] || done[2] {
		t.Fatalf("recorded %v, want only v1", done)
	}

	// Fixing v2 and re-applying picks up where it left off.
	ms[1].SQL = "CREATE TABLE accounts_green (id INT)"
	applied, err = Apply(ctx, d, ms)
	if err != nil || !reflect.DeepEqual(applied, []int{2}) {
		t.Fatalf("re-Apply: applied=%v err=%v, want [2]", applied, err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		ms   []Migration
		want string
	}{
		{"empty", nil, "no migrations"},
		{"zero version", []Migration{{0, "a", Expand, "SELECT 1"}}, "strictly increasing"},
		{"out of order", []Migration{{2, "a", Expand, "SELECT 1"}, {1, "b", Expand, "SELECT 1"}}, "strictly increasing"},
		{"duplicate", []Migration{{1, "a", Expand, "SELECT 1"}, {1, "b", Expand, "SELECT 1"}}, "strictly increasing"},
		{"no name", []Migration{{1, "", Expand, "SELECT 1"}}, "missing name"},
		{"no phase", []Migration{{1, "a", 0, "SELECT 1"}}, "unknown phase"},
		{"lint", []Migration{{1, "a", Expand, "DROP TABLE t"}}, "contains DROP"},
		{"ok", []Migration{{1, "a", Expand, "SELECT 1"}, {5, "b", Contract, "DROP TABLE t"}}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(c.ms)
			if c.want == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want containing %q", err, c.want)
			}
		})
	}
}
