"""Integration tests against a running GizmoSQL Docker container.

These tests require Docker to be running and are marked with
``@pytest.mark.integration``. Skip them with:
    pytest -m "not integration"
"""

from __future__ import annotations

import pytest

pytestmark = pytest.mark.integration


@pytest.fixture(scope="session")
def conn(gizmosql_server, gizmosql_uri):
    """Create a DBAPI connection to the test GizmoSQL server."""
    from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

    from adbc_driver_gizmosql import dbapi as gizmosql

    with gizmosql.connect(
        gizmosql_uri,
        username=GIZMOSQL_USERNAME,
        password=GIZMOSQL_PASSWORD,
        tls_skip_verify=True,
    ) as connection:
        yield connection


class TestPasswordAuth:
    """Test password-based authentication and basic queries."""

    def test_select_one(self, conn):
        with conn.cursor() as cur:
            cur.execute("SELECT 1 AS value")
            table = cur.fetch_arrow_table()
            assert table.num_rows == 1
            assert table.column("value")[0].as_py() == 1

    def test_gizmosql_version(self, conn):
        with conn.cursor() as cur:
            cur.execute("SELECT GIZMOSQL_VERSION() AS version")
            table = cur.fetch_arrow_table()
            assert table.num_rows == 1
            version = table.column("version")[0].as_py()
            assert isinstance(version, str)
            assert len(version) > 0

    def test_parameterized_query(self, conn):
        with conn.cursor() as cur:
            cur.execute(
                "SELECT n_nationkey, n_name FROM nation WHERE n_nationkey = ?",
                parameters=[24],
            )
            table = cur.fetch_arrow_table()
            assert table.num_rows == 1
            assert table.column("n_nationkey")[0].as_py() == 24

    def test_fetch_arrow_table_type(self, conn):
        import pyarrow as pa

        with conn.cursor() as cur:
            cur.execute("SELECT 1 AS a, 'hello' AS b")
            table = cur.fetch_arrow_table()
            assert isinstance(table, pa.Table)
            assert table.schema.names == ["a", "b"]

    def test_multiple_rows(self, conn):
        with conn.cursor() as cur:
            cur.execute("SELECT * FROM nation ORDER BY n_nationkey LIMIT 5")
            table = cur.fetch_arrow_table()
            assert table.num_rows == 5


class TestExecuteAutoDetect:
    """Test cursor.execute() auto-detecting DDL/DML for immediate execution."""

    def test_create_insert_query_drop(self, conn):
        """execute() handles the full DDL/DML/SELECT lifecycle."""
        with conn.cursor() as cur:
            cur.execute("CREATE TABLE test_auto_detect (id INT, name VARCHAR)")

        try:
            with conn.cursor() as cur:
                cur.execute("INSERT INTO test_auto_detect VALUES (1, 'alice')")
                cur.execute("INSERT INTO test_auto_detect VALUES (2, 'bob')")

            with conn.cursor() as cur:
                cur.execute("SELECT id, name FROM test_auto_detect ORDER BY id")
                table = cur.fetch_arrow_table()
                assert table.num_rows == 2
                assert table.column("id")[0].as_py() == 1
                assert table.column("name")[1].as_py() == "bob"
        finally:
            with conn.cursor() as cur:
                cur.execute("DROP TABLE test_auto_detect")

    def test_description_none_after_ddl(self, conn):
        """execute() returns None description for DDL (DBAPI 2.0 spec)."""
        with conn.cursor() as cur:
            cur.execute("CREATE TABLE test_desc_ddl (id INT)")
        try:
            with conn.cursor() as cur:
                cur.execute("DROP TABLE test_desc_ddl")
                assert cur.description is None
        except Exception:
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE IF EXISTS test_desc_ddl")
            raise

    def test_description_present_after_select(self, conn):
        """execute() returns column descriptions for SELECT."""
        with conn.cursor() as cur:
            cur.execute("SELECT 1 AS a, 'hello' AS b")
            assert cur.description is not None
            assert len(cur.description) == 2

    def test_chaining(self, conn):
        """execute() returns self for method chaining."""
        with conn.cursor() as cur:
            result = cur.execute("SELECT 1")
            assert result is cur


class TestExecuteUpdate:
    """Test cursor.execute_update() for explicit DDL/DML with rows-affected count."""

    def test_create_insert_query_drop(self, conn):
        with conn.cursor() as cur:
            # DDL — CREATE TABLE
            result = cur.execute_update("CREATE TABLE test_exec_update (id INT, name VARCHAR)")
            assert result == 0

        try:
            with conn.cursor() as cur:
                # DML — INSERT single row
                rows = cur.execute_update(
                    "INSERT INTO test_exec_update VALUES (1, 'alice')",
                )
                assert rows == 1

                # DML — INSERT another row
                rows = cur.execute_update(
                    "INSERT INTO test_exec_update VALUES (2, 'bob')",
                )
                assert rows == 1

            # Verify the data was actually written
            with conn.cursor() as cur:
                cur.execute("SELECT id, name FROM test_exec_update ORDER BY id")
                table = cur.fetch_arrow_table()
                assert table.num_rows == 2
                assert table.column("id")[0].as_py() == 1
                assert table.column("name")[1].as_py() == "bob"
        finally:
            # Clean up
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE test_exec_update")

    def test_update_returns_rows_affected(self, conn):
        with conn.cursor() as cur:
            cur.execute_update("CREATE TABLE test_eu_update (val INT)")

        try:
            with conn.cursor() as cur:
                cur.execute_update("INSERT INTO test_eu_update VALUES (1)")
                cur.execute_update("INSERT INTO test_eu_update VALUES (2)")
                cur.execute_update("INSERT INTO test_eu_update VALUES (3)")

            with conn.cursor() as cur:
                rows = cur.execute_update("DELETE FROM test_eu_update WHERE val >= 2")
                assert rows == 2

            # Verify only the expected row survives
            with conn.cursor() as cur:
                cur.execute("SELECT val FROM test_eu_update ORDER BY val")
                table = cur.fetch_arrow_table()
                assert table.num_rows == 1
                assert table.column("val")[0].as_py() == 1
        finally:
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE test_eu_update")

    def test_module_level_execute_update_backward_compat(self, conn):
        """Verify the module-level execute_update() shim still works."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        with conn.cursor() as cur:
            result = gizmosql.execute_update(cur, "CREATE TABLE test_eu_compat (id INT)")
            assert isinstance(result, int)

        try:
            with conn.cursor() as cur:
                rows = gizmosql.execute_update(cur, "INSERT INTO test_eu_compat VALUES (1)")
                assert rows == 1
        finally:
            with conn.cursor() as cur:
                gizmosql.execute_update(cur, "DROP TABLE test_eu_compat")


class TestBulkIngest:
    """Test ADBC bulk ingest (adbc_ingest) for loading Arrow data into tables."""

    def test_ingest_create(self, conn):
        """Test mode='create' — creates a new table and inserts data."""
        import pyarrow as pa

        table = pa.table(
            {
                "id": [1, 2, 3],
                "name": ["alice", "bob", "charlie"],
            }
        )

        with conn.cursor() as cur:
            cur.adbc_ingest("test_ingest_create", table, mode="create")

        try:
            with conn.cursor() as cur:
                cur.execute("SELECT id, name FROM test_ingest_create ORDER BY id")
                result = cur.fetch_arrow_table()
                assert result.num_rows == 3
                assert result.column("id").to_pylist() == [1, 2, 3]
                assert result.column("name").to_pylist() == ["alice", "bob", "charlie"]
        finally:
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE test_ingest_create")

    def test_ingest_append(self, conn):
        """Test mode='append' — appends data to an existing table."""
        import pyarrow as pa

        with conn.cursor() as cur:
            cur.execute_update("CREATE TABLE test_ingest_append (id BIGINT, val DOUBLE)")

        try:
            batch1 = pa.table({"id": [1, 2], "val": [10.0, 20.0]})
            batch2 = pa.table({"id": [3, 4], "val": [30.0, 40.0]})

            with conn.cursor() as cur:
                cur.adbc_ingest("test_ingest_append", batch1, mode="append")
                cur.adbc_ingest("test_ingest_append", batch2, mode="append")

            with conn.cursor() as cur:
                cur.execute("SELECT id, val FROM test_ingest_append ORDER BY id")
                result = cur.fetch_arrow_table()
                assert result.num_rows == 4
                assert result.column("id").to_pylist() == [1, 2, 3, 4]
                assert result.column("val").to_pylist() == [10.0, 20.0, 30.0, 40.0]
        finally:
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE test_ingest_append")

    def test_ingest_create_append(self, conn):
        """Test mode='create_append' — creates if needed, then appends."""
        import pyarrow as pa

        table = pa.table({"x": [100, 200]})

        # First call creates the table
        with conn.cursor() as cur:
            cur.adbc_ingest("test_ingest_ca", table, mode="create_append")

        try:
            # Second call appends to the existing table
            with conn.cursor() as cur:
                cur.adbc_ingest("test_ingest_ca", table, mode="create_append")

            with conn.cursor() as cur:
                cur.execute("SELECT x FROM test_ingest_ca ORDER BY x")
                result = cur.fetch_arrow_table()
                assert result.num_rows == 4
                assert result.column("x").to_pylist() == [100, 100, 200, 200]
        finally:
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE test_ingest_ca")

    def test_ingest_replace(self, conn):
        """Test mode='replace' — drops and recreates the table."""
        import pyarrow as pa

        original = pa.table({"a": [1, 2, 3]})
        replacement = pa.table({"a": [99]})

        with conn.cursor() as cur:
            cur.adbc_ingest("test_ingest_replace", original, mode="create")

        try:
            with conn.cursor() as cur:
                cur.adbc_ingest("test_ingest_replace", replacement, mode="replace")

            with conn.cursor() as cur:
                cur.execute("SELECT a FROM test_ingest_replace")
                result = cur.fetch_arrow_table()
                assert result.num_rows == 1
                assert result.column("a")[0].as_py() == 99
        finally:
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE test_ingest_replace")

    def test_ingest_record_batch(self, conn):
        """Test ingesting a single RecordBatch (not a full Table)."""
        import pyarrow as pa

        batch = pa.record_batch({"id": [10, 20], "label": ["foo", "bar"]})

        with conn.cursor() as cur:
            cur.adbc_ingest("test_ingest_rb", batch, mode="create")

        try:
            with conn.cursor() as cur:
                cur.execute("SELECT id, label FROM test_ingest_rb ORDER BY id")
                result = cur.fetch_arrow_table()
                assert result.num_rows == 2
                assert result.column("id").to_pylist() == [10, 20]
                assert result.column("label").to_pylist() == ["foo", "bar"]
        finally:
            with conn.cursor() as cur:
                cur.execute_update("DROP TABLE test_ingest_rb")


class TestReturningClause:
    """End-to-end tests for INSERT/UPDATE/DELETE ... RETURNING.

    Regression for issue #163: ``INSERT ... RETURNING`` was being routed
    through ``execute_update()`` (DoPut), which only returns a row count
    and discards the ``RETURNING`` rows. The cursor must instead take the
    regular query path so ``fetch_arrow_table()`` exposes the returned
    rows.
    """

    def test_insert_returning_yields_rows_AND_persists(self, conn):
        # Two-part check:
        #   1. the RETURNING rows surface through fetch_arrow_table()
        #      (was lost in the DoPut path before the fix), and
        #   2. the INSERT/UPDATE/DELETE actually persisted to the table.
        # Part 2 matters because GizmoSQL's GetFlightInfo only PLANS the
        # query — actual execution happens at DoGet (fetch). Hence why
        # plain DDL/DML still routes through execute_update(). For
        # statements that travel through the query path we still need
        # the caller to fetch, and this test confirms the fetch fully
        # commits the change as DuckDB would for a normal query.
        with conn.cursor() as cur:
            cur.execute_update(
                query=(
                    "CREATE SEQUENCE test_returning_seq; "
                    "CREATE TABLE test_returning ("
                    "    id INTEGER DEFAULT nextval('test_returning_seq') PRIMARY KEY,"
                    "    name VARCHAR"
                    ")"
                )
            )

        try:
            # ---- INSERT ... RETURNING ----
            with conn.cursor() as cur:
                cur.execute(
                    operation=(
                        "INSERT INTO test_returning (name) VALUES "
                        "('alice'), ('bob') RETURNING id, name"
                    )
                )
                # description must reflect a real result set (was None before fix)
                assert cur.description is not None
                returned = cur.fetch_arrow_table()
                assert returned.num_rows == 2
                returned_names = set(returned.column("name").to_pylist())
                assert returned_names == {"alice", "bob"}
                # IDs are sequence-generated; just verify both are non-null
                # and unique.
                returned_ids = returned.column("id").to_pylist()
                assert all(isinstance(i, int) for i in returned_ids)
                assert len(set(returned_ids)) == 2

            # The INSERT must have actually persisted: a fresh cursor
            # should see the same two rows.
            with conn.cursor() as cur:
                cur.execute(operation="SELECT id, name FROM test_returning ORDER BY id")
                persisted = cur.fetch_arrow_table()
                assert persisted.num_rows == 2
                persisted_names = set(persisted.column("name").to_pylist())
                assert persisted_names == {"alice", "bob"}
                # IDs returned to the client must match the IDs in the table.
                persisted_ids = set(persisted.column("id").to_pylist())
                assert persisted_ids == set(returned_ids)

            # ---- UPDATE ... RETURNING ----
            with conn.cursor() as cur:
                cur.execute(
                    operation=(
                        "UPDATE test_returning SET name = 'CAROL' "
                        "WHERE name = 'alice' RETURNING id, name"
                    )
                )
                updated_rows = cur.fetch_arrow_table()
                assert updated_rows.num_rows == 1
                assert updated_rows.column("name")[0].as_py() == "CAROL"

            # The UPDATE must have actually persisted.
            with conn.cursor() as cur:
                cur.execute(operation="SELECT name FROM test_returning ORDER BY name")
                after_update = cur.fetch_arrow_table()
                assert after_update.column("name").to_pylist() == ["CAROL", "bob"]

            # ---- DELETE ... RETURNING ----
            with conn.cursor() as cur:
                cur.execute(
                    operation=("DELETE FROM test_returning WHERE name = 'bob' RETURNING id")
                )
                deleted_rows = cur.fetch_arrow_table()
                assert deleted_rows.num_rows == 1

            # The DELETE must have actually persisted.
            with conn.cursor() as cur:
                cur.execute(operation="SELECT name FROM test_returning")
                after_delete = cur.fetch_arrow_table()
                assert after_delete.num_rows == 1
                assert after_delete.column("name")[0].as_py() == "CAROL"
        finally:
            with conn.cursor() as cur:
                cur.execute_update(
                    query=("DROP TABLE test_returning; DROP SEQUENCE test_returning_seq")
                )

    def test_insert_returning_persists_even_without_fetch(self, conn):
        # The reason the original keyword-based DDL/DML split exists is that
        # GizmoSQL's GetFlightInfo only PLANS — actual execution waits for
        # DoGet. An INSERT routed through the regular query path would never
        # fire if the caller didn't fetch.
        #
        # The RETURNING carve-out reintroduces the query-path route, which
        # would re-open this foot-gun unless we eagerly materialize. The
        # cursor must guarantee the DML fires regardless of whether the
        # caller calls fetch_arrow_table(). This test asserts that
        # invariant: do an INSERT...RETURNING, deliberately skip the fetch,
        # and confirm the row landed.
        with conn.cursor() as cur:
            cur.execute_update(query="CREATE TABLE test_returning_no_fetch (id INT)")
        try:
            with conn.cursor() as cur:
                cur.execute(
                    operation=("INSERT INTO test_returning_no_fetch VALUES (1), (2) RETURNING id")
                )
                # The cursor must expose the eagerly-materialized state
                # without us having to call fetch_arrow_table().
                assert cur.description is not None
                assert cur.rowcount == 2
                # And we MUST NOT call fetch_arrow_table() here — the whole
                # point is that the INSERT must already have happened.

            # New cursor: the rows must be persisted.
            with conn.cursor() as cur:
                cur.execute(operation="SELECT id FROM test_returning_no_fetch ORDER BY id")
                rows = cur.fetch_arrow_table()
                assert rows.column("id").to_pylist() == [1, 2]

            # The cached result is still readable on the original cursor's
            # successor — verify by re-issuing and fetching.
            with conn.cursor() as cur:
                cur.execute(
                    operation=("INSERT INTO test_returning_no_fetch VALUES (3) RETURNING id")
                )
                returned = cur.fetch_arrow_table()
                assert returned.column("id").to_pylist() == [3]
        finally:
            with conn.cursor() as cur:
                cur.execute_update(query="DROP TABLE test_returning_no_fetch")

    def test_insert_without_returning_still_uses_doput_and_persists(self, conn):
        # Sanity check: a plain INSERT (no RETURNING) must still take the
        # execute_update path — cursor.description stays None and the
        # rowcount comes back populated. This is the original reason for the
        # DDL/DML keyword split (#1): GizmoSQL's lazy GetFlightInfo means a
        # plain INSERT via the query path would never execute unless the
        # caller fetched. This test guards against the RETURNING carve-out
        # accidentally widening to all DML AND confirms the rows persist.
        with conn.cursor() as cur:
            cur.execute_update(query="CREATE TABLE test_no_returning (id INT)")
        try:
            with conn.cursor() as cur:
                cur.execute(operation="INSERT INTO test_no_returning VALUES (1), (2)")
                assert cur.description is None
                assert cur.rowcount == 2

            # And the rows must actually be in the table.
            with conn.cursor() as cur:
                cur.execute(operation="SELECT id FROM test_no_returning ORDER BY id")
                rows = cur.fetch_arrow_table()
                assert rows.column("id").to_pylist() == [1, 2]
        finally:
            with conn.cursor() as cur:
                cur.execute_update(query="DROP TABLE test_no_returning")


class TestConnectionContextManager:
    """Test that the connection works properly as a context manager."""

    def test_fresh_connection(self, gizmosql_server, gizmosql_uri):
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        from adbc_driver_gizmosql import dbapi as gizmosql

        with gizmosql.connect(
            gizmosql_uri,
            username=GIZMOSQL_USERNAME,
            password=GIZMOSQL_PASSWORD,
            tls_skip_verify=True,
        ) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 42 AS answer")
                table = cur.fetch_arrow_table()
                assert table.column("answer")[0].as_py() == 42


class TestConnectionProfiles:
    """Test ADBC connection profiles (driver manager >= 1.11.0).

    Profiles are TOML files resolved by the ADBC driver manager at database
    init time. These tests write real profile files and connect through them
    against the live test server, covering named-profile resolution via
    ADBC_PROFILE_PATH, absolute-path profiles, profile:// URIs, env-var
    substitution for secrets, and application-option precedence.
    """

    @pytest.fixture()
    def profile_dir(self, tmp_path, monkeypatch, gizmosql_uri):
        """Write a complete GizmoSQL profile and put its directory on
        ADBC_PROFILE_PATH so it resolves by bare name."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        profile = tmp_path / "gizmosql_test.toml"
        profile.write_text(
            "profile_version = 1\n"
            "\n"
            "[Options]\n"
            f'uri = "{gizmosql_uri}"\n'
            f'username = "{GIZMOSQL_USERNAME}"\n'
            f'password = "{GIZMOSQL_PASSWORD}"\n'
            '"adbc.flight.sql.client_option.tls_skip_verify" = "true"\n'
        )
        monkeypatch.setenv("ADBC_PROFILE_PATH", str(tmp_path))
        return tmp_path

    def test_named_profile(self, gizmosql_server, profile_dir):
        """connect(profile=<name>) resolves the profile via ADBC_PROFILE_PATH
        and needs no uri or credentials in Python."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        with gizmosql.connect(profile="gizmosql_test") as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 1 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 1

    def test_profile_uri_scheme(self, gizmosql_server, profile_dir):
        """connect('profile://<name>') is equivalent to profile=<name>."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        with gizmosql.connect("profile://gizmosql_test") as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 2 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 2

    def test_absolute_path_profile(self, gizmosql_server, profile_dir):
        """connect(profile=</abs/path/to.toml>) loads the file directly,
        without any search-path configuration."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        profile_path = profile_dir / "gizmosql_test.toml"
        with gizmosql.connect(profile=str(profile_path)) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 3 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 3

    def test_profile_env_var_substitution(
        self, gizmosql_server, gizmosql_uri, tmp_path, monkeypatch
    ):
        """Secrets can be kept out of the profile via {{ env_var(NAME) }}."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        from adbc_driver_gizmosql import dbapi as gizmosql

        monkeypatch.setenv("GIZMOSQL_TEST_USERNAME", GIZMOSQL_USERNAME)
        monkeypatch.setenv("GIZMOSQL_TEST_PASSWORD", GIZMOSQL_PASSWORD)
        profile = tmp_path / "gizmosql_env.toml"
        profile.write_text(
            "profile_version = 1\n"
            "\n"
            "[Options]\n"
            f'uri = "{gizmosql_uri}"\n'
            'username = "{{ env_var(GIZMOSQL_TEST_USERNAME) }}"\n'
            'password = "{{ env_var(GIZMOSQL_TEST_PASSWORD) }}"\n'
            '"adbc.flight.sql.client_option.tls_skip_verify" = "true"\n'
        )

        with gizmosql.connect(profile=str(profile)) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 4 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 4

    def test_profile_env_var_missing_substitutes_empty(
        self, gizmosql_server, gizmosql_uri, tmp_path, monkeypatch
    ):
        """A {{ env_var(NAME) }} referencing an unset variable substitutes an
        empty string (documented driver-manager behavior), so authentication
        must fail rather than silently succeed."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        monkeypatch.delenv("GIZMOSQL_TEST_UNSET_PASSWORD", raising=False)
        profile = tmp_path / "gizmosql_unset_env.toml"
        profile.write_text(
            "profile_version = 1\n"
            "\n"
            "[Options]\n"
            f'uri = "{gizmosql_uri}"\n'
            'username = "gizmosql_username"\n'
            'password = "{{ env_var(GIZMOSQL_TEST_UNSET_PASSWORD) }}"\n'
            '"adbc.flight.sql.client_option.tls_skip_verify" = "true"\n'
        )

        with pytest.raises(Exception):
            with gizmosql.connect(profile=str(profile)) as conn:
                with conn.cursor() as cur:
                    cur.execute("SELECT 1")
                    cur.fetch_arrow_table()

    def test_explicit_options_override_profile(
        self, gizmosql_server, gizmosql_uri, tmp_path, monkeypatch
    ):
        """Options set in Python take precedence over the profile's [Options]:
        a profile with bad credentials still connects when good credentials
        are passed to connect()."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        from adbc_driver_gizmosql import dbapi as gizmosql

        profile = tmp_path / "gizmosql_bad_creds.toml"
        profile.write_text(
            "profile_version = 1\n"
            "\n"
            "[Options]\n"
            f'uri = "{gizmosql_uri}"\n'
            'username = "wrong_user"\n'
            'password = "wrong_password"\n'
            '"adbc.flight.sql.client_option.tls_skip_verify" = "true"\n'
        )

        with gizmosql.connect(
            profile=str(profile),
            username=GIZMOSQL_USERNAME,
            password=GIZMOSQL_PASSWORD,
        ) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 5 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 5

    def test_explicit_uri_overrides_profile_with_profile_kwarg(
        self, gizmosql_server, gizmosql_uri, profile_dir
    ):
        """uri= and profile= can be combined; the explicit uri wins."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        with gizmosql.connect(gizmosql_uri, profile="gizmosql_test") as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 6 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 6

    def test_ddl_dml_routing_through_profile_connection(self, gizmosql_server, profile_dir):
        """The GizmoSQL Cursor DDL/DML auto-routing works on a
        profile-established connection."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        with gizmosql.connect(profile="gizmosql_test") as conn:
            with conn.cursor() as cur:
                cur.execute("CREATE TABLE test_profile_ddl (id INT)")
            try:
                with conn.cursor() as cur:
                    cur.execute("INSERT INTO test_profile_ddl VALUES (1), (2)")
                    assert cur.rowcount == 2
                with conn.cursor() as cur:
                    cur.execute("SELECT COUNT(*) AS n FROM test_profile_ddl")
                    assert cur.fetch_arrow_table().column("n")[0].as_py() == 2
            finally:
                with conn.cursor() as cur:
                    cur.execute_update("DROP TABLE test_profile_ddl")

    def test_missing_profile_raises(self, gizmosql_server, profile_dir):
        """A profile name that doesn't resolve raises a driver-manager error."""
        from adbc_driver_gizmosql import dbapi as gizmosql

        with pytest.raises(Exception, match="(?i)profile"):
            gizmosql.connect(profile="does_not_exist_profile")


class TestGizmoSqlUriScheme:
    """The gizmosql:// URI scheme (mapped onto flightsql://, added upstream in 1.12.0)."""

    def test_connect_with_gizmosql_uri(self, gizmosql_server):
        """gizmosql://host:port defaults to TLS and connects like grpc+tls://."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        from adbc_driver_gizmosql import dbapi as gizmosql

        uri = f"gizmosql://{gizmosql_server.host}:{gizmosql_server.port}"
        with gizmosql.connect(
            uri,
            username=GIZMOSQL_USERNAME,
            password=GIZMOSQL_PASSWORD,
            tls_skip_verify=True,
        ) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 1 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 1

    def test_ddl_dml_routing_with_gizmosql_uri(self, gizmosql_server):
        """DDL/DML auto-detection works over a gizmosql:// connection."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        from adbc_driver_gizmosql import dbapi as gizmosql

        uri = f"gizmosql://{gizmosql_server.host}:{gizmosql_server.port}"
        with gizmosql.connect(
            uri,
            username=GIZMOSQL_USERNAME,
            password=GIZMOSQL_PASSWORD,
            tls_skip_verify=True,
        ) as conn:
            with conn.cursor() as cur:
                cur.execute("CREATE TABLE test_gizmosql_uri (id INT)")
            try:
                with conn.cursor() as cur:
                    cur.execute("INSERT INTO test_gizmosql_uri VALUES (1), (2)")
                    assert cur.rowcount == 2
            finally:
                with conn.cursor() as cur:
                    cur.execute_update("DROP TABLE test_gizmosql_uri")

    def test_connect_with_flightsql_uri_passthrough(self, gizmosql_server):
        """The underlying flightsql:// scheme still works when given directly."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        from adbc_driver_gizmosql import dbapi as gizmosql

        uri = f"flightsql://{gizmosql_server.host}:{gizmosql_server.port}"
        with gizmosql.connect(
            uri,
            username=GIZMOSQL_USERNAME,
            password=GIZMOSQL_PASSWORD,
            tls_skip_verify=True,
        ) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 1 AS value")
                assert cur.fetch_arrow_table().column("value")[0].as_py() == 1


class TestOpenTelemetryTracing:
    """OpenTelemetry tracing wired into the Flight SQL query path in 1.12.0."""

    def test_adbcfile_exporter_writes_traces(self, gizmosql_server, gizmosql_uri, tmp_path):
        """Passing adbc.telemetry.* options via db_kwargs produces trace files."""
        from conftest import GIZMOSQL_PASSWORD, GIZMOSQL_USERNAME

        from adbc_driver_gizmosql import dbapi as gizmosql

        traces_dir = tmp_path / "traces"
        traces_dir.mkdir()
        with gizmosql.connect(
            gizmosql_uri,
            username=GIZMOSQL_USERNAME,
            password=GIZMOSQL_PASSWORD,
            tls_skip_verify=True,
            db_kwargs={
                "adbc.telemetry.traces_exporter": "adbcfile",
                "adbc.telemetry.traces_folder_path": str(traces_dir),
            },
        ) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 1 AS value")
                cur.fetch_arrow_table()
        trace_files = list(traces_dir.iterdir())
        assert trace_files, "expected the adbcfile exporter to write trace output"
        contents = "".join(f.read_text() for f in trace_files)
        assert "ExecuteQuery" in contents or "execute" in contents.lower()
