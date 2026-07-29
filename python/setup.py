"""Wheel configuration for adbc-driver-gizmosql.

The package is pure Python but ships the prebuilt Go shared library
(``libadbc_driver_gizmosql``), so wheels must be platform-specific while
staying ``py3-none`` (no CPython ABI dependency) — the same tagging
scheme the upstream ADBC Python packages use, e.g.
``adbc_driver_gizmosql-2.0.0-py3-none-macosx_11_0_arm64.whl``.
"""

from setuptools import setup

try:
    from setuptools.command.bdist_wheel import bdist_wheel
except ImportError:  # setuptools < 70.1
    from wheel.bdist_wheel import bdist_wheel


class BinaryBdistWheel(bdist_wheel):
    def finalize_options(self):
        super().finalize_options()
        # Not pure: the bundled shared library is platform-specific.
        self.root_is_pure = False

    def get_tag(self):
        _, _, plat = super().get_tag()
        return "py3", "none", plat


setup(cmdclass={"bdist_wheel": BinaryBdistWheel})
