# Copyright (c) 2025 ADBC Drivers Contributors
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

import adbc_drivers_validation.tests.connection as connection_tests

from . import vendor_bugs
from .gizmosql import get_quirks


def pytest_generate_tests(metafunc) -> None:
    quirks = [get_quirks(metafunc.config.getoption("vendor_version"))]
    return connection_tests.generate_tests(quirks, metafunc)


class TestConnection(connection_tests.TestConnection):
    def test_set_current_catalog(self, driver, conn_factory) -> None:
        vendor_bugs.expect_vendor_bug(
            lambda: super(TestConnection, self).test_set_current_catalog(
                driver=driver, conn_factory=conn_factory
            ),
            reason=vendor_bugs.WRONG_STATUS_NOT_FOUND,
            raises=AssertionError,
            match=r"INVALID_ARGUMENT.*No catalog \+ schema named \"thiscatalogdoesnotexist\"",
        )

    def test_set_current_schema(self, driver, conn_factory) -> None:
        vendor_bugs.expect_vendor_bug(
            lambda: super(TestConnection, self).test_set_current_schema(
                driver=driver, conn_factory=conn_factory
            ),
            reason=vendor_bugs.WRONG_STATUS_NOT_FOUND,
            raises=AssertionError,
            match=r"INVALID_ARGUMENT.*No catalog \+ schema named \"thisschemadoesnotexist\"",
        )
