package db

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "  \n ; ; ", nil},
		{"two plain", "CREATE TABLE a (id INT); CREATE TABLE b (id INT)",
			[]string{"CREATE TABLE a (id INT)", "CREATE TABLE b (id INT)"}},
		{"semicolon in literal", `INSERT INTO t VALUES ('a;b'); SELECT 1`,
			[]string{`INSERT INTO t VALUES ('a;b')`, `SELECT 1`}},
		{"escaped quote", `INSERT INTO t VALUES ('it''s;'); SELECT 1`,
			[]string{`INSERT INTO t VALUES ('it''s;')`, `SELECT 1`}},
		{"quoted identifier", `SELECT "a;b" FROM t; SELECT 2`,
			[]string{`SELECT "a;b" FROM t`, `SELECT 2`}},
		{"line comment", "SELECT 1; -- trailing; comment\nSELECT 2",
			[]string{"SELECT 1", "-- trailing; comment\nSELECT 2"}},
		{"block comment", "SELECT 1 /* a; b */; SELECT 2",
			[]string{"SELECT 1 /* a; b */", "SELECT 2"}},
		{"dollar quoted", "CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql; SELECT 3",
			[]string{"CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql", "SELECT 3"}},
		{"tagged dollar quoted", "SELECT $x$ a; $$ b $x$; SELECT 4",
			[]string{"SELECT $x$ a; $$ b $x$", "SELECT 4"}},
		{"trigger body", "CREATE TRIGGER tr INSTEAD OF INSERT ON v BEGIN INSERT INTO t VALUES (1); INSERT INTO u VALUES (2); END; SELECT 5",
			[]string{"CREATE TRIGGER tr INSTEAD OF INSERT ON v BEGIN INSERT INTO t VALUES (1); INSERT INTO u VALUES (2); END", "SELECT 5"}},
		{"case end inside trigger", "CREATE TRIGGER tr BEFORE INSERT ON t BEGIN SELECT CASE WHEN 1 THEN 2 END; END; SELECT 6",
			[]string{"CREATE TRIGGER tr BEFORE INSERT ON t BEGIN SELECT CASE WHEN 1 THEN 2 END; END", "SELECT 6"}},
		{"begin transaction is not a block", "BEGIN; INSERT INTO t VALUES (1); COMMIT",
			[]string{"BEGIN", "INSERT INTO t VALUES (1)", "COMMIT"}},
		{"begin work", "BEGIN WORK; SELECT 1; COMMIT",
			[]string{"BEGIN WORK", "SELECT 1", "COMMIT"}},
		{"identifier containing keyword", "SELECT begin_at, case_no, endpoint FROM t; SELECT 7",
			[]string{"SELECT begin_at, case_no, endpoint FROM t", "SELECT 7"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SplitStatements(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}
