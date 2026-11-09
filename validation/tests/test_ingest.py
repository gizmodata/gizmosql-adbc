# Copyright (c) 2025-2026 ADBC Drivers Contributors
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
# Modified by GizmoData LLC: GizmoSQL quirks and known server bugs.

import adbc_drivers_validation.tests.ingest as ingest_tests

from . import vendor_bugs
from .gizmosql import get_quirks


def pytest_generate_tests(metafunc) -> None:
    quirks = [get_quirks(metafunc.config.getoption("vendor_version"))]
    return ingest_tests.generate_tests(quirks, metafunc)


class TestIngest(ingest_tests.TestIngest):
    def test_create_conflict(self, driver, conn, query) -> None:
        vendor_bugs.expect_vendor_bug(
            lambda: super(TestIngest, self).test_create_conflict(
                driver=driver, conn=conn, query=query
            ),
            reason=vendor_bugs.WRONG_STATUS_ALREADY_EXISTS,
            raises=AssertionError,
            match=r"INVALID_ARGUMENT.*already exists \(InvalidArgument; ",
        )

    def test_append_schema_mismatch(self, driver, conn, query) -> None:
        vendor_bugs.expect_vendor_bug(
            lambda: super(TestIngest, self).test_append_schema_mismatch(
                driver=driver, conn=conn, query=query
            ),
            reason=vendor_bugs.WRONG_STATUS_ALREADY_EXISTS,
            raises=AssertionError,
            match=r"INVALID_ARGUMENT.*column with name \"adbc_extra_column\"",
        )

    def test_createappend_schema_mismatch(self, driver, conn, query) -> None:
        vendor_bugs.expect_vendor_bug(
            lambda: super(TestIngest, self).test_createappend_schema_mismatch(
                driver=driver, conn=conn, query=query
            ),
            reason=vendor_bugs.WRONG_STATUS_ALREADY_EXISTS,
            raises=AssertionError,
            match=r"INVALID_ARGUMENT.*column with name \"adbc_extra_column\"",
        )

    def test_temporary(self, driver, conn_factory, query) -> None:
        # Symptom: the rows ingested as non-temporary show up in the
        # temporary table (the first comparison) instead of the normal one.
        vendor_bugs.expect_vendor_bug(
            lambda: super(TestIngest, self).test_temporary(
                driver=driver, conn_factory=conn_factory, query=query
            ),
            reason=vendor_bugs.INGEST_UNQUALIFIED_TARGET,
            raises=AssertionError,
            match=r"Tables do not match!.*\n\+\{'idx': 1, ",
        )
