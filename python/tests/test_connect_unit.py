"""Unit tests for the dbapi connect function (no server required).

Adapted from the 1.x suite: the tests and their assertions are the same,
but the mock seams moved — 2.0 has no adbc-driver-flightsql layer, so
every database is built via ``adbc_driver_manager.AdbcDatabase`` with the
bundled Go driver, and the ``gizmosql://`` → ``flightsql://`` rewrite
(asserted here in 1.x) now happens inside the Go driver, covered by its
own unit tests and by the verbatim integration suite.
"""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest

from adbc_driver_gizmosql._oauth import OAuthResult

# connect() builds: AdbcDatabase (with driver=<bundled lib>) → AdbcConnection
# → our Connection wrapper. _driver_path is patched so unit tests never
# need the compiled library.
_PATCH_DB = "adbc_driver_gizmosql.dbapi.adbc_driver_manager.AdbcDatabase"
_PATCH_CONN = "adbc_driver_gizmosql.dbapi.adbc_driver_manager.AdbcConnection"
_PATCH_CLS = "adbc_driver_gizmosql.dbapi.Connection"
_PATCH_LIB = "adbc_driver_gizmosql.dbapi._driver_path"

FAKE_LIB = "/fake/libadbc_driver_gizmosql.dylib"


def _db_kwargs(mock_db) -> dict:
    """The option map handed to AdbcDatabase (minus the driver path)."""
    kwargs = dict(mock_db.call_args[1])
    kwargs.pop("driver", None)
    return kwargs


@patch(_PATCH_LIB, return_value=FAKE_LIB)
@patch(_PATCH_CLS)
@patch(_PATCH_CONN)
@patch(_PATCH_DB)
class TestConnect:
    """Tests for dbapi.connect()."""

    def test_password_auth(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        result = connect(
            "grpc+tls://localhost:31337",
            username="myuser",
            password="mypass",
            tls_skip_verify=True,
        )

        assert result == mock_conn_cls.return_value
        mock_db.assert_called_once()
        kwargs = _db_kwargs(mock_db)
        assert kwargs["uri"] == "grpc+tls://localhost:31337"
        assert kwargs["username"] == "myuser"
        assert kwargs["password"] == "mypass"
        assert "adbc.flight.sql.client_option.tls_skip_verify" in kwargs
        assert mock_db.call_args[1]["driver"] == FAKE_LIB

    def test_password_auth_no_tls_skip(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect(
            "grpc+tls://localhost:31337",
            username="myuser",
            password="mypass",
        )

        assert "adbc.flight.sql.client_option.tls_skip_verify" not in _db_kwargs(mock_db)

    @patch("adbc_driver_gizmosql.dbapi.get_oauth_token")
    def test_external_auth(self, mock_oauth, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_oauth.return_value = OAuthResult(
            token="eyJ-id-token-from-idp",
            session_uuid="test-uuid",
        )
        mock_conn_cls.return_value = MagicMock()

        result = connect(
            "grpc+tls://localhost:31337",
            auth_type="external",
            tls_skip_verify=True,
        )

        assert result == mock_conn_cls.return_value
        mock_oauth.assert_called_once_with(
            host="localhost",
            port=31339,
            tls_skip_verify=True,
            timeout=300,
            open_browser=True,
            oauth_url=None,
        )
        kwargs = _db_kwargs(mock_db)
        assert kwargs["username"] == "token"
        assert kwargs["password"] == "eyJ-id-token-from-idp"

    @patch("adbc_driver_gizmosql.dbapi.get_oauth_token")
    def test_external_auth_custom_oauth_port(
        self, mock_oauth, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib
    ):
        from adbc_driver_gizmosql.dbapi import connect

        mock_oauth.return_value = OAuthResult(token="jwt", session_uuid="uuid")
        mock_conn_cls.return_value = MagicMock()

        connect(
            "grpc+tls://myserver.example.com:31337",
            auth_type="external",
            oauth_port=8443,
            oauth_tls_skip_verify=False,
            tls_skip_verify=True,
        )

        mock_oauth.assert_called_once_with(
            host="myserver.example.com",
            port=8443,
            tls_skip_verify=False,
            timeout=300,
            open_browser=True,
            oauth_url=None,
        )

    @patch("adbc_driver_gizmosql.dbapi.get_oauth_token")
    def test_external_auth_explicit_oauth_url(
        self, mock_oauth, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib
    ):
        from adbc_driver_gizmosql.dbapi import connect

        mock_oauth.return_value = OAuthResult(token="jwt", session_uuid="uuid")
        mock_conn_cls.return_value = MagicMock()

        connect(
            "grpc+tls://localhost:31337",
            auth_type="external",
            oauth_url="https://oauth.example.com:9999",
        )

        mock_oauth.assert_called_once()
        assert mock_oauth.call_args[1]["oauth_url"] == "https://oauth.example.com:9999"

    def test_invalid_auth_type(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        with pytest.raises(ValueError, match="Invalid auth_type"):
            connect("grpc+tls://localhost:31337", auth_type="kerberos")

    def test_db_kwargs_passthrough(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect(
            "grpc+tls://localhost:31337",
            username="user",
            password="pass",
            db_kwargs={"custom_option": "custom_value"},
        )

        kwargs = _db_kwargs(mock_db)
        assert kwargs["custom_option"] == "custom_value"
        assert kwargs["username"] == "user"

    def test_conn_kwargs_passthrough(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect(
            "grpc+tls://localhost:31337",
            username="user",
            password="pass",
            conn_kwargs={"conn_option": "conn_value"},
        )

        # conn_kwargs are unpacked into AdbcConnection
        mock_adbc_conn.assert_called_once_with(
            mock_db.return_value,
            conn_option="conn_value",
        )

    def test_autocommit_default_true(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect("grpc+tls://localhost:31337", username="u", password="p")

        mock_conn_cls.assert_called_once_with(
            mock_db.return_value,
            mock_adbc_conn.return_value,
            autocommit=True,
        )


@patch(_PATCH_LIB, return_value=FAKE_LIB)
@patch(_PATCH_CLS)
@patch(_PATCH_CONN)
@patch(_PATCH_DB)
class TestGizmoSqlUriPassthrough:
    """gizmosql:// URIs pass straight through to the Go driver, which owns
    the flightsql:// rewrite (asserted in its own unit tests and by the
    verbatim integration suite)."""

    def test_gizmosql_scheme_passed_through(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()
        connect("gizmosql://localhost:31337", username="u", password="p")
        assert _db_kwargs(mock_db)["uri"] == "gizmosql://localhost:31337"

    def test_query_params_preserved(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()
        connect("gizmosql://localhost:31337?transport=tcp", username="u", password="p")
        assert _db_kwargs(mock_db)["uri"] == "gizmosql://localhost:31337?transport=tcp"

    def test_other_schemes_untouched(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()
        for uri in (
            "grpc+tls://localhost:31337",
            "grpc+tcp://localhost:31337",
            "flightsql://localhost:31337",
        ):
            connect(uri, username="u", password="p")
            assert _db_kwargs(mock_db)["uri"] == uri


@patch(_PATCH_LIB, return_value=FAKE_LIB)
@patch(_PATCH_CLS)
@patch(_PATCH_CONN)
@patch(_PATCH_DB)
class TestConnectProfile:
    """Tests for ADBC connection profile support in dbapi.connect()."""

    def test_no_uri_and_no_profile_raises(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        with pytest.raises(ValueError, match="'uri' or 'profile'"):
            connect()

    def test_profile_only(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        """Profile-only connect() builds the database with the bundled Go
        driver and the profile option."""
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        result = connect(profile="gizmosql_dev")

        assert result == mock_conn_cls.return_value
        mock_db.assert_called_once()
        kwargs = mock_db.call_args[1]
        assert kwargs["profile"] == "gizmosql_dev"
        assert kwargs["driver"] == FAKE_LIB
        assert "uri" not in kwargs

    def test_profile_only_explicit_credentials_override(
        self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib
    ):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect(profile="gizmosql_dev", username="override_user", password="override_pass")

        kwargs = _db_kwargs(mock_db)
        assert kwargs["username"] == "override_user"
        assert kwargs["password"] == "override_pass"

    def test_uri_with_profile(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect("grpc+tls://localhost:31337", profile="gizmosql_dev")

        kwargs = _db_kwargs(mock_db)
        assert kwargs["profile"] == "gizmosql_dev"
        assert kwargs["uri"] == "grpc+tls://localhost:31337"

    def test_profile_uri_scheme_passthrough(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        """A profile:// URI is equivalent to profile=<name>."""
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect("profile://gizmosql_dev")

        kwargs = _db_kwargs(mock_db)
        assert kwargs["profile"] == "gizmosql_dev"
        assert "uri" not in kwargs

    def test_profile_only_tls_skip_verify(self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib):
        from adbc_driver_gizmosql.dbapi import connect

        mock_conn_cls.return_value = MagicMock()

        connect(profile="gizmosql_dev", tls_skip_verify=True)

        kwargs = _db_kwargs(mock_db)
        assert kwargs["adbc.flight.sql.client_option.tls_skip_verify"] == "true"

    def test_external_auth_profile_only_requires_oauth_url(
        self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib
    ):
        from adbc_driver_gizmosql.dbapi import connect

        with pytest.raises(ValueError, match="oauth_url"):
            connect(profile="gizmosql_dev", auth_type="external")

    def test_external_auth_profile_uri_requires_oauth_url(
        self, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib
    ):
        from adbc_driver_gizmosql.dbapi import connect

        with pytest.raises(ValueError, match="oauth_url"):
            connect("profile://gizmosql_dev", auth_type="external")

    @patch("adbc_driver_gizmosql.dbapi.get_oauth_token")
    def test_external_auth_profile_with_oauth_url(
        self, mock_oauth, mock_db, mock_adbc_conn, mock_conn_cls, mock_lib
    ):
        """OAuth works with a profile-only connection when oauth_url is given."""
        from adbc_driver_gizmosql.dbapi import connect

        mock_oauth.return_value = OAuthResult(token="jwt", session_uuid="uuid")
        mock_conn_cls.return_value = MagicMock()

        connect(
            profile="gizmosql_dev",
            auth_type="external",
            oauth_url="https://oauth.example.com:31339",
        )

        mock_oauth.assert_called_once()
        assert mock_oauth.call_args[1]["oauth_url"] == "https://oauth.example.com:31339"
        kwargs = _db_kwargs(mock_db)
        assert kwargs["username"] == "token"
        assert kwargs["password"] == "jwt"


class TestExecuteUpdate:
    """Tests for the module-level execute_update() backward-compat shim."""

    def test_execute_update_calls_adbc_statement(self):
        from adbc_driver_gizmosql.dbapi import execute_update

        mock_cursor = MagicMock()
        mock_cursor.execute_update = lambda q: __import__(
            "adbc_driver_gizmosql.dbapi", fromlist=["Cursor"]
        ).Cursor.execute_update(mock_cursor, q)
        mock_cursor.adbc_statement.execute_update.return_value = 42

        result = execute_update(mock_cursor, "INSERT INTO t VALUES (1)")

        mock_cursor.adbc_statement.set_sql_query.assert_called_once_with("INSERT INTO t VALUES (1)")
        mock_cursor.adbc_statement.execute_update.assert_called_once()
        assert result == 42

    def test_execute_update_propagates_exception(self):
        from adbc_driver_gizmosql.dbapi import Cursor, execute_update

        mock_cursor = MagicMock(spec=Cursor)
        mock_cursor.adbc_statement = MagicMock()
        mock_cursor.execute_update = lambda q: Cursor.execute_update(mock_cursor, q)
        mock_cursor.adbc_statement.execute_update.side_effect = RuntimeError("server error")

        with pytest.raises(RuntimeError, match="server error"):
            execute_update(mock_cursor, "DROP TABLE nonexistent")


class TestCursorExecuteShim:
    """Cursor.execute() collapses the Go driver's empty-schema results so
    description is None after DDL/DML (1.x parity). The routing itself
    happens inside the Go driver."""

    @patch("adbc_driver_gizmosql.dbapi._BaseCursor.execute")
    def test_empty_schema_result_collapsed(self, mock_super_execute):
        from adbc_driver_gizmosql.dbapi import Cursor

        cursor = MagicMock(spec=Cursor)
        results = MagicMock()
        results.description = []  # empty schema → DDL/DML marker
        cursor._results = results

        result = Cursor.execute(cursor, "CREATE TABLE t (a INT)")

        mock_super_execute.assert_called_once_with("CREATE TABLE t (a INT)", None)
        results.close.assert_called_once()
        assert cursor._results is None
        assert result is cursor

    @patch("adbc_driver_gizmosql.dbapi._BaseCursor.execute")
    def test_result_with_columns_kept(self, mock_super_execute):
        from adbc_driver_gizmosql.dbapi import Cursor

        cursor = MagicMock(spec=Cursor)
        results = MagicMock()
        results.description = [("v", None, None, None, None, None, None)]
        cursor._results = results

        result = Cursor.execute(cursor, "SELECT 1 AS v")

        mock_super_execute.assert_called_once_with("SELECT 1 AS v", None)
        results.close.assert_not_called()
        assert cursor._results is results
        assert result is cursor


class TestCursorExecuteUpdate:
    """Tests for Cursor.execute_update() method."""

    def test_execute_update_calls_adbc_statement(self):
        from adbc_driver_gizmosql.dbapi import Cursor

        cursor = MagicMock(spec=Cursor)
        cursor.adbc_statement = MagicMock()
        cursor.adbc_statement.execute_update.return_value = 3

        result = Cursor.execute_update(cursor, "INSERT INTO t VALUES (1)")

        cursor.adbc_statement.set_sql_query.assert_called_once_with("INSERT INTO t VALUES (1)")
        cursor.adbc_statement.execute_update.assert_called_once()
        assert result == 3

    def test_execute_update_ddl(self):
        from adbc_driver_gizmosql.dbapi import Cursor

        cursor = MagicMock(spec=Cursor)
        cursor.adbc_statement = MagicMock()
        cursor.adbc_statement.execute_update.return_value = 0

        result = Cursor.execute_update(cursor, "CREATE TABLE t (a INT)")

        assert result == 0


class TestExtractHost:
    """Tests for URI host extraction."""

    def test_grpc_tls(self):
        from adbc_driver_gizmosql.dbapi import _extract_host

        assert _extract_host("grpc+tls://localhost:31337") == "localhost"

    def test_grpc_plain(self):
        from adbc_driver_gizmosql.dbapi import _extract_host

        assert _extract_host("grpc://myhost:31337") == "myhost"

    def test_hostname_with_domain(self):
        from adbc_driver_gizmosql.dbapi import _extract_host

        assert _extract_host("grpc+tls://gizmosql.example.com:31337") == "gizmosql.example.com"

    def test_no_port(self):
        from adbc_driver_gizmosql.dbapi import _extract_host

        assert _extract_host("grpc+tls://localhost") == "localhost"

    def test_grpc_tcp(self):
        from adbc_driver_gizmosql.dbapi import _extract_host

        assert _extract_host("grpc+tcp://192.168.1.1:31337") == "192.168.1.1"

    def test_gizmosql_scheme(self):
        from adbc_driver_gizmosql.dbapi import _extract_host

        assert _extract_host("gizmosql://gizmosql.example.com:31337") == "gizmosql.example.com"

    def test_query_string_stripped(self):
        from adbc_driver_gizmosql.dbapi import _extract_host

        assert _extract_host("gizmosql://myhost:31337?transport=tcp") == "myhost"
        assert _extract_host("gizmosql://myhost?transport=tcp") == "myhost"


# Verbatim from the 1.x suite — the helpers are retained in 2.0.
class TestIsDdlDml:
    """Tests for _is_ddl_dml() SQL keyword detection."""

    def test_create_table(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("CREATE TABLE t (a INT)") is True

    def test_drop_table(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("DROP TABLE t") is True

    def test_insert(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("INSERT INTO t VALUES (1)") is True

    def test_update(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("UPDATE t SET a = 1") is True

    def test_delete(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("DELETE FROM t WHERE a = 1") is True

    def test_alter(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("ALTER TABLE t ADD COLUMN b INT") is True

    def test_select_is_not_ddl_dml(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("SELECT 1") is False

    def test_with_cte_is_not_ddl_dml(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("WITH cte AS (SELECT 1) SELECT * FROM cte") is False

    def test_show_is_not_ddl_dml(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("SHOW TABLES") is False

    def test_leading_whitespace(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("   CREATE TABLE t (a INT)") is True

    def test_lowercase(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("create table t (a INT)") is True

    def test_mixed_case(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("Create Table t (a INT)") is True

    def test_empty_string(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("") is False

    def test_bytes_not_ddl_dml(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml(b"CREATE TABLE t (a INT)") is False

    def test_truncate(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("TRUNCATE TABLE t") is True

    def test_merge(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("MERGE INTO t USING s ON t.id = s.id") is True

    def test_copy(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("COPY t FROM '/tmp/data.csv'") is True

    def test_block_comment_before_create(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml('/* {"app": "dbt"} */\nCREATE TABLE t (a INT)') is True

    def test_block_comment_before_alter(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("/* comment */ ALTER TABLE t ADD COLUMN b INT") is True

    def test_block_comment_before_select(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("/* comment */ SELECT 1") is False

    def test_line_comment_before_insert(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("-- some comment\nINSERT INTO t VALUES (1)") is True

    def test_multiple_block_comments(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("/* a */ /* b */ DROP TABLE t") is True

    def test_only_comment(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        assert _is_ddl_dml("/* just a comment */") is False

    def test_dbt_query_comment_multiline(self):
        from adbc_driver_gizmosql.dbapi import _is_ddl_dml

        sql = (
            '/* {"app": "dbt", "dbt_version": "1.11.7", '
            '"profile_name": "test", "target_name": "dev"} */\n'
            'create view "memory"."main"."my_model" as (\n'
            "  select 1\n"
            ");"
        )
        assert _is_ddl_dml(sql) is True
