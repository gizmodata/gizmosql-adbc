"""Unit tests for the SQL classification helpers in dbapi.

These tests cover the routing decision that picks between the
``execute_update`` (DoPut) path and the regular ``GetFlightInfo``→``DoGet``
path. They do not touch a real GizmoSQL server.
"""

from __future__ import annotations

import pytest

from adbc_driver_gizmosql.dbapi import (
    _has_returning_clause,
    _is_ddl_dml,
    _strip_sql_comments,
)


class TestHasReturningClause:
    """Direct tests for the RETURNING-detection helper."""

    @pytest.mark.parametrize(
        "sql",
        [
            "INSERT INTO t (name) VALUES ('a') RETURNING id",
            "insert into t values ('a') returning id, name",
            "INSERT INTO t VALUES ('a')\nRETURNING id",
            "UPDATE t SET x = 1 WHERE id = 1 RETURNING *",
            "DELETE FROM t WHERE id = 1 RETURNING id",
            "INSERT INTO t VALUES ('a')\n  Returning id;",
        ],
    )
    def test_detects_returning(self, sql):
        assert _has_returning_clause(sql) is True

    @pytest.mark.parametrize(
        "sql",
        [
            "INSERT INTO t VALUES (1)",
            "UPDATE t SET x = 1 WHERE id = 1",
            "DELETE FROM t WHERE id = 1",
            "CREATE TABLE t (id INT)",
            "SELECT * FROM t",
        ],
    )
    def test_no_returning(self, sql):
        assert _has_returning_clause(sql) is False

    def test_returning_inside_string_literal_is_ignored(self):
        # 'returning' is the column value, not a clause.
        assert (
            _has_returning_clause("INSERT INTO t (msg) VALUES ('returning')")
            is False
        )

    def test_returning_inside_double_quoted_identifier_is_ignored(self):
        # A column literally named "returning" — DuckDB-legal with quoting.
        assert (
            _has_returning_clause(
                'INSERT INTO t ("returning") VALUES (1)'
            )
            is False
        )

    def test_returning_with_escaped_inner_quote(self):
        # Escaped single quote ('') inside a literal must not break parsing.
        assert (
            _has_returning_clause(
                "INSERT INTO t (msg) VALUES ('it''s returning')"
            )
            is False
        )

    def test_returning_after_string_literal_still_detected(self):
        sql = "INSERT INTO t (msg) VALUES ('returning') RETURNING id"
        assert _has_returning_clause(sql) is True


class TestIsDdlDml:
    """The composite classifier used by Cursor.execute()."""

    @pytest.mark.parametrize(
        "sql",
        [
            "CREATE TABLE t (id INT)",
            "INSERT INTO t VALUES (1)",
            "UPDATE t SET x = 1",
            "DELETE FROM t",
            "ALTER TABLE t ADD COLUMN y INT",
            "DROP TABLE t",
            "SET autoinstall_known_extensions = true",
            # Comment-prefixed (e.g. dbt-style query tag) — still classified.
            "/* {\"app\": \"dbt\"} */\nINSERT INTO t VALUES (1)",
            "-- a comment\nUPDATE t SET x = 1",
        ],
    )
    def test_classified_as_ddl_dml(self, sql):
        assert _is_ddl_dml(sql) is True

    @pytest.mark.parametrize(
        "sql",
        [
            # Read queries — never DDL/DML routing.
            "SELECT 1",
            "WITH cte AS (SELECT 1) SELECT * FROM cte",
            "SHOW TABLES",
            "EXPLAIN SELECT 1",
        ],
    )
    def test_not_ddl_dml(self, sql):
        assert _is_ddl_dml(sql) is False

    @pytest.mark.parametrize(
        "sql",
        [
            "INSERT INTO t (name) VALUES ('a') RETURNING id",
            "UPDATE t SET x = 1 WHERE id = 1 RETURNING *",
            "DELETE FROM t WHERE id = 1 RETURNING id",
            "/* tag */ INSERT INTO t VALUES (1) RETURNING id",
            "-- log\nUPDATE t SET x = 1 RETURNING x",
        ],
    )
    def test_returning_falls_through_to_query_path(self, sql):
        # Regression for issue #163: INSERT/UPDATE/DELETE with RETURNING must
        # NOT be classified as plain DDL/DML (which would route through
        # execute_update and silently discard the returned rows). The cursor
        # must take the regular query path so fetch_arrow_table() works.
        assert _is_ddl_dml(sql) is False

    def test_non_string_input(self):
        # Substrait plans arrive as bytes — they are not classified here.
        assert _is_ddl_dml(b"CREATE TABLE t (id INT)") is False
        assert _is_ddl_dml(None) is False


class TestStripSqlComments:
    """Lightweight checks on the comment-stripping helper used upstream."""

    def test_block_comment(self):
        assert (
            _strip_sql_comments("/* hello */ SELECT 1").strip() == "SELECT 1"
        )

    def test_line_comment(self):
        assert (
            _strip_sql_comments("-- hello\nSELECT 1").strip() == "SELECT 1"
        )

    def test_no_comment(self):
        assert _strip_sql_comments("SELECT 1") == "SELECT 1"
