# Copyright (c) 2026 ADBC Drivers Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#         http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Modified by GizmoData LLC: adapted from the adbc-drivers/quack validation
# quirks for the GizmoSQL (Arrow Flight SQL over DuckDB) driver.

"""Driver quirks for running the adbc-drivers.org validation suite against
GizmoSQL.

GizmoSQL runs DuckDB behind Arrow Flight SQL, so the SQL dialect is DuckDB's
and most type overrides mirror the DuckDB-based Quack driver's.  Every feature
set to False below, and every override under ``validation/queries``, carries
the reason it is not supported.
"""

import functools
import os
import re
from pathlib import Path

from adbc_drivers_validation import model, quirks

# Connection settings come from project-prefixed variables only (never the
# generic GIZMOSQL_USERNAME/GIZMOSQL_PASSWORD, which may hold real
# credentials in a developer's shell).  Defaults match the CI service
# container and the local `docker run` in validation/README.md.
URI_ENV = "GIZMOSQL_ADBC_TEST_URI"
USERNAME_ENV = "GIZMOSQL_ADBC_TEST_USERNAME"
PASSWORD_ENV = "GIZMOSQL_ADBC_TEST_PASSWORD"
DEFAULTS = {
    URI_ENV: "grpc://127.0.0.1:31447",
    USERNAME_ENV: "gizmosql_user",
    PASSWORD_ENV: "gizmosql_password",
}
for _env, _value in DEFAULTS.items():
    os.environ.setdefault(_env, _value)

# Created by the session fixture in conftest.py before any test runs.
SECONDARY_SCHEMA = "validation_secondary_schema"
SECONDARY_CATALOG = "validation_secondary"


class GizmoSQLQuirks(model.DriverQuirks):
    name = "gizmosql"
    driver = "adbc_driver_gizmosql"
    driver_name = "ADBC GizmoSQL Driver - Go"
    vendor_name = "gizmosql"
    # GizmoSQL reports its engine version (stable channel: DuckDB 1.5.x).
    vendor_version = re.compile(r"duckdb v1\.5\.\d+")
    short_version = "1.5"
    features = model.DriverFeatures(
        connection_get_table_schema=True,
        connection_set_current_catalog=True,
        connection_set_current_schema=True,
        connection_transactions=True,
        current_catalog="memory",
        current_schema="main",
        get_objects=True,
        get_objects_tabletypes_filter=True,
        # Not supported (the upstream Flight SQL driver's GetObjects leaves
        # these empty): constraints (check/foreign/primary/unique), the
        # xdbc_* column fields (so test_not_null skips), GetStatistics, and
        # per-field type-name metadata.
        statement_bind=True,
        statement_bulk_ingest=True,
        statement_bulk_ingest_catalog=True,
        statement_bulk_ingest_schema=True,
        statement_bulk_ingest_temporary=True,
        statement_execute_schema=True,
        statement_get_parameter_schema=True,
        statement_prepare=True,
        statement_rows_affected=True,
        statement_rows_affected_ddl=True,
        secondary_catalog=SECONDARY_CATALOG,
        secondary_catalog_schema="main",
        secondary_schema=SECONDARY_SCHEMA,
        supported_xdbc_fields=[],
    )
    setup = model.DriverSetup(
        database={
            "uri": model.FromEnv(URI_ENV),
            "username": model.FromEnv(USERNAME_ENV),
            "password": model.FromEnv(PASSWORD_ENV),
        },
        connection={},
        statement={},
    )

    @property
    def queries_paths(self) -> tuple[Path]:
        return (Path(__file__).parent.parent / "queries",)

    def is_table_not_found(self, table_name: str | None, error: Exception) -> bool:
        error_text = str(error).lower()
        found = (
            "does not exist" in error_text
            or "not found" in error_text
            or "no such table" in error_text
        )
        if table_name is None:
            return found
        return found and table_name.lower() in error_text

    def split_statement(self, statement: str) -> list[str]:
        return quirks.split_statement(statement, dialect="duckdb")

    # DuckDB keeps temporary tables in their own catalog ("temp", schema
    # "main"), separate from the session's current catalog and schema.
    def qualify_temp_table(self, cursor, name: str) -> list[str]:
        return ["temp", "main", name]

    def drop_table(
        self,
        *,
        table_name: str,
        schema_name: str | None = None,
        catalog_name: str | None = None,
        if_exists: bool = True,
        temporary: bool = False,
    ) -> str:
        if temporary:
            name = self.quote_identifier("temp", "main", table_name)
            return f"DROP TABLE {'IF EXISTS ' if if_exists else ''}{name}"
        return super().drop_table(
            table_name=table_name,
            schema_name=schema_name,
            catalog_name=catalog_name,
            if_exists=if_exists,
        )


@functools.cache
def get_quirks(version: str) -> GizmoSQLQuirks:
    gizmosql_quirks = GizmoSQLQuirks()
    if version != gizmosql_quirks.short_version:
        raise ValueError(f"Unsupported GizmoSQL engine version: {version}")
    return gizmosql_quirks
