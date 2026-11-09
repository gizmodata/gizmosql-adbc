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
# conftest; adds the fail-not-skip server check and the secondary
# catalog/schema setup.

import os
import sys
from pathlib import Path

import adbc_driver_manager.dbapi
import adbc_drivers_validation.model
import adbc_drivers_validation.tests.conftest
import pytest
from adbc_drivers_validation.tests.conftest import (  # noqa: F401
    conn,
    conn_factory,
    db_kwargs,
    manual_test,
    noci,
    pytest_collection_modifyitems,
)

from .gizmosql import (
    PASSWORD_ENV,
    SECONDARY_CATALOG,
    SECONDARY_SCHEMA,
    URI_ENV,
    USERNAME_ENV,
    get_quirks,
)

REPO_ROOT = Path(__file__).parent.parent.parent
DRIVER_PATH_ENV = "GIZMOSQL_ADBC_TEST_DRIVER_PATH"


def pytest_addoption(parser):
    adbc_drivers_validation.tests.conftest.pytest_addoption(parser)
    parser.addoption("--vendor-version", action="store", default="1.5")


def _driver_path() -> str:
    if path := os.environ.get(DRIVER_PATH_ENV):
        return path
    ext = {"win32": "dll", "darwin": "dylib"}.get(sys.platform, "so")
    return str(REPO_ROOT / f"go/build/libadbc_driver_gizmosql.{ext}")


def _db_kwargs() -> dict[str, str]:
    return {
        "uri": os.environ[URI_ENV],
        "username": os.environ[USERNAME_ENV],
        "password": os.environ[PASSWORD_ENV],
    }


def pytest_sessionstart(session: pytest.Session) -> None:
    """Fail the run (never skip it) when the driver or server is missing.

    This suite gates releases: a missing library or an unreachable server
    must turn the job red, not produce a wall of skips and exit 0.  The same
    connection creates the secondary schema and catalog the catalog/schema
    tests switch to.
    """
    if session.config.getoption("--collect-only"):
        return
    path = _driver_path()
    if not Path(path).is_file():
        pytest.exit(
            f"driver library not found at {path} (build it with `make -C go lib` "
            f"or set {DRIVER_PATH_ENV})",
            returncode=3,
        )
    try:
        with (
            adbc_driver_manager.dbapi.connect(
                driver=path, db_kwargs=_db_kwargs(), autocommit=True
            ) as conn,
            conn.cursor() as cursor,
        ):
            cursor.execute(operation=f"CREATE SCHEMA IF NOT EXISTS {SECONDARY_SCHEMA}")
            cursor.execute(operation=f"ATTACH IF NOT EXISTS ':memory:' AS {SECONDARY_CATALOG}")
    except Exception as e:
        pytest.exit(
            f"cannot reach the GizmoSQL server at {os.environ[URI_ENV]}: {e}",
            returncode=3,
        )


_outcomes = {"passed": 0}


def pytest_runtest_logreport(report: pytest.TestReport) -> None:
    if report.when == "call" and report.passed:
        _outcomes["passed"] += 1


def pytest_sessionfinish(session: pytest.Session, exitstatus: int) -> None:
    # Zero passing tests is a failure, whatever the reason (all skipped,
    # nothing collected, a filter that matched nothing).
    if session.config.getoption("--collect-only"):
        return
    if exitstatus == 0 and _outcomes["passed"] == 0:
        session.exitstatus = 1


@pytest.fixture(scope="session")
def driver(request, pytestconfig) -> adbc_drivers_validation.model.DriverQuirks:
    driver = request.param
    assert driver.startswith("gizmosql")
    return get_quirks(pytestconfig.getoption("vendor_version"))


@pytest.fixture(scope="session")
def driver_path(driver: adbc_drivers_validation.model.DriverQuirks) -> str:
    return _driver_path()
