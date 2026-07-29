"""Wheel configuration for adbc-driver-gizmosql.

The package is pure Python but ships the prebuilt Go shared library
(``libadbc_driver_gizmosql``), so wheels must be platform-specific while
staying ``py3-none`` (no CPython ABI dependency) — the same tagging
scheme the upstream ADBC Python packages use, e.g.
``adbc_driver_gizmosql-2.0.0-py3-none-macosx_11_0_arm64.whl``.

``has_ext_modules`` (not just ``root_is_pure``) is the load-bearing
override: it makes setuptools treat the install layout as platlib, so
package files land at the wheel root instead of a ``.data/purelib/``
tree — which is required for a shared library to be legal in the wheel
(and for auditwheel to repair the Linux builds).
"""

from setuptools import setup
from setuptools.dist import Distribution

try:
    from setuptools.command.bdist_wheel import bdist_wheel
except ImportError:  # setuptools < 70.1
    from wheel.bdist_wheel import bdist_wheel


class BinaryDistribution(Distribution):
    def has_ext_modules(self):
        return True


class BinaryBdistWheel(bdist_wheel):
    def get_tag(self):
        _, _, plat = super().get_tag()
        return "py3", "none", plat


setup(distclass=BinaryDistribution, cmdclass={"bdist_wheel": BinaryBdistWheel})
