// Licensed under the Apache License, Version 2.0.

// Ported from the 1.x Python driver's tests/test_sql_routing_unit.py —
// these cases are the parity spec for the routing decision that picks
// between the ExecuteUpdate (DoPut) path and the regular
// GetFlightInfo→DoGet path.

package gizmosql

import (
	"strings"
	"testing"
)

func TestHasReturningClauseDetects(t *testing.T) {
	for _, sql := range []string{
		"INSERT INTO t (name) VALUES ('a') RETURNING id",
		"insert into t values ('a') returning id, name",
		"INSERT INTO t VALUES ('a')\nRETURNING id",
		"UPDATE t SET x = 1 WHERE id = 1 RETURNING *",
		"DELETE FROM t WHERE id = 1 RETURNING id",
		"INSERT INTO t VALUES ('a')\n  Returning id;",
	} {
		if !hasReturningClause(sql) {
			t.Errorf("hasReturningClause(%q) = false, want true", sql)
		}
	}
}

func TestHasReturningClauseNegative(t *testing.T) {
	for _, sql := range []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET x = 1 WHERE id = 1",
		"DELETE FROM t WHERE id = 1",
		"CREATE TABLE t (id INT)",
		"SELECT * FROM t",
		// 'returning' is the column value, not a clause.
		"INSERT INTO t (msg) VALUES ('returning')",
		// A column literally named "returning" — DuckDB-legal with quoting.
		`INSERT INTO t ("returning") VALUES (1)`,
		// Escaped single quote ('') inside a literal must not break parsing.
		"INSERT INTO t (msg) VALUES ('it''s returning')",
	} {
		if hasReturningClause(sql) {
			t.Errorf("hasReturningClause(%q) = true, want false", sql)
		}
	}
}

func TestHasReturningClauseAfterStringLiteral(t *testing.T) {
	sql := "INSERT INTO t (msg) VALUES ('returning') RETURNING id"
	if !hasReturningClause(sql) {
		t.Errorf("hasReturningClause(%q) = false, want true", sql)
	}
}

func TestIsDDLDMLClassified(t *testing.T) {
	for _, sql := range []string{
		"CREATE TABLE t (id INT)",
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET x = 1",
		"DELETE FROM t",
		"ALTER TABLE t ADD COLUMN y INT",
		"DROP TABLE t",
		"SET autoinstall_known_extensions = true",
		// Comment-prefixed (e.g. dbt-style query tag) — still classified.
		"/* {\"app\": \"dbt\"} */\nINSERT INTO t VALUES (1)",
		"-- a comment\nUPDATE t SET x = 1",
	} {
		if !isDDLDML(sql) {
			t.Errorf("isDDLDML(%q) = false, want true", sql)
		}
	}
}

func TestIsDDLDMLNegative(t *testing.T) {
	for _, sql := range []string{
		// Read queries — never DDL/DML routing.
		"SELECT 1",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"SHOW TABLES",
		"EXPLAIN SELECT 1",
		// Empty / comment-only input.
		"",
		"   ",
		"/* only a comment */",
	} {
		if isDDLDML(sql) {
			t.Errorf("isDDLDML(%q) = true, want false", sql)
		}
	}
}

func TestIsDDLDMLReturningFallsThroughToQueryPath(t *testing.T) {
	// Regression parity for 1.x issue #3 / gizmosql#163:
	// INSERT/UPDATE/DELETE with RETURNING must NOT be classified as plain
	// DDL/DML (which would route through ExecuteUpdate and silently
	// discard the returned rows).
	for _, sql := range []string{
		"INSERT INTO t (name) VALUES ('a') RETURNING id",
		"UPDATE t SET x = 1 WHERE id = 1 RETURNING *",
		"DELETE FROM t WHERE id = 1 RETURNING id",
		"/* tag */ INSERT INTO t VALUES (1) RETURNING id",
		"-- log\nUPDATE t SET x = 1 RETURNING x",
	} {
		if isDDLDML(sql) {
			t.Errorf("isDDLDML(%q) = true, want false (RETURNING carve-out)", sql)
		}
	}
}

func TestStripSQLComments(t *testing.T) {
	if got := strings.TrimSpace(stripSQLComments("/* hello */ SELECT 1")); got != "SELECT 1" {
		t.Errorf("block comment: got %q", got)
	}
	if got := strings.TrimSpace(stripSQLComments("-- hello\nSELECT 1")); got != "SELECT 1" {
		t.Errorf("line comment: got %q", got)
	}
	if got := stripSQLComments("SELECT 1"); got != "SELECT 1" {
		t.Errorf("no comment: got %q", got)
	}
}
