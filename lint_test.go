package db

import (
	"errors"
	"testing"
)

func TestLint(t *testing.T) {
	cases := []struct {
		name   string
		phase  Phase
		sql    string
		wantOp string // "" means clean
	}{
		{"create table", Expand, "CREATE TABLE a (id INT)", ""},
		{"add column", Expand, "ALTER TABLE a ADD COLUMN b INT", ""},
		{"create view", Expand, "CREATE VIEW v AS SELECT id FROM a", ""},
		{"create index", Expand, "CREATE INDEX ix ON a (id)", ""},
		{"drop not null is fine", Expand, "ALTER TABLE a ALTER COLUMN b DROP NOT NULL", ""},
		{"drop default is fine", Expand, "ALTER TABLE a ALTER COLUMN b DROP DEFAULT", ""},
		{"set default is fine", Expand, "ALTER TABLE a ALTER COLUMN b SET DEFAULT 0", ""},
		{"insert with scary literal", Expand, "INSERT INTO a (n) VALUES ('DROP TABLE a; RENAME x')", ""},
		{"scary comment", Expand, "-- DROP TABLE a\nSELECT 1 /* TRUNCATE b */", ""},
		{"scary dollar quote", Expand, "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 /* TRUNCATE b */ $$ LANGUAGE sql", ""},
		{"column named rename_at", Expand, "ALTER TABLE a ADD COLUMN rename_at TIMESTAMP", ""},
		{"update is fine", Expand, "UPDATE a SET b = 1 WHERE b IS NULL", ""},

		{"drop table", Expand, "DROP TABLE a", "DROP"},
		{"drop column", Expand, "ALTER TABLE a DROP COLUMN b", "DROP"},
		{"drop index", Expand, "DROP INDEX ix", "DROP"},
		{"drop view", Expand, "DROP VIEW v", "DROP VIEW"},
		{"drop view if exists", Expand, "DROP VIEW IF EXISTS v", "DROP VIEW"},
		{"rename table", Expand, "ALTER TABLE a RENAME TO b", "RENAME"},
		{"rename column", Expand, "ALTER TABLE a RENAME COLUMN b TO c", "RENAME"},
		{"retype column", Expand, "ALTER TABLE a ALTER COLUMN b TYPE BIGINT", "ALTER COLUMN TYPE"},
		{"retype column set data", Expand, "ALTER TABLE a ALTER b SET DATA TYPE BIGINT", "ALTER COLUMN TYPE"},
		{"truncate", Expand, "TRUNCATE a", "TRUNCATE"},
		{"delete", Expand, "DELETE FROM a WHERE 1=1", "DELETE"},
		{"second statement", Expand, "CREATE TABLE a (id INT); DROP TABLE b", "DROP"},
		{"lowercase", Expand, "drop table a", "DROP"},

		{"cutover may drop view", Cutover, "DROP VIEW v; CREATE VIEW v AS SELECT id FROM a2", ""},
		{"cutover may not drop table", Cutover, "DROP VIEW v; DROP TABLE a", "DROP"},
		{"cutover may not rename", Cutover, "ALTER TABLE a RENAME TO b", "RENAME"},

		{"contract anything", Contract, "DROP TABLE a; TRUNCATE b; ALTER TABLE c RENAME TO d", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Lint(c.phase, c.sql)
			if c.wantOp == "" {
				if err != nil {
					t.Fatalf("want clean, got %v", err)
				}
				return
			}
			var le *LintError
			if !errors.As(err, &le) {
				t.Fatalf("want LintError, got %v", err)
			}
			if le.Operation != c.wantOp || le.Phase != c.phase {
				t.Fatalf("got %s/%s, want %s/%s: %v", le.Phase, le.Operation, c.phase, c.wantOp, err)
			}
		})
	}
}

func TestPhaseString(t *testing.T) {
	for p, want := range map[Phase]string{Expand: "expand", Cutover: "cutover", Contract: "contract", Phase(9): "Phase(9)"} {
		if got := p.String(); got != want {
			t.Errorf("%d: got %q want %q", int(p), got, want)
		}
	}
}
