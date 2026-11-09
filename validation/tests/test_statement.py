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
# Modified by GizmoData LLC: GizmoSQL quirks and known server bugs.

import adbc_driver_manager
import adbc_drivers_validation.tests.statement as statement_tests

from . import vendor_bugs
from .gizmosql import get_quirks


def pytest_generate_tests(metafunc) -> None:
    quirks = [get_quirks(metafunc.config.getoption("vendor_version"))]
    return statement_tests.generate_tests(quirks, metafunc)


class TestStatement(statement_tests.TestStatement):
    def test_parameter_execute(self, driver, conn) -> None:
        vendor_bugs.expect_vendor_bug(
            lambda: super(TestStatement, self).test_parameter_execute(driver=driver, conn=conn),
            reason=vendor_bugs.QUERY_PARAMETER_ROWS,
            raises=adbc_driver_manager.ProgrammingError,
            match=r"received 4 rows of bind parameters; a query accepts exactly one row",
        )
