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

import pathlib
import platform
import struct

from setuptools import setup
from setuptools.dist import Distribution

try:
    from setuptools.command.bdist_wheel import bdist_wheel
except ImportError:  # setuptools < 70.1
    from wheel.bdist_wheel import bdist_wheel


class BinaryDistribution(Distribution):
    def has_ext_modules(self):
        return True


_PACKAGE_DIR = pathlib.Path(__file__).resolve().parent / "src" / "adbc_driver_gizmosql"
_MACHO_CPU = {0x01000007: "x86_64", 0x0100000C: "arm64"}
_LC_VERSION_MIN_MACOSX = 0x24
_LC_BUILD_VERSION = 0x32


def _macho_tag(lib: pathlib.Path) -> str:
    """Return the wheel platform tag (``macosx_12_0_arm64``) that describes ``lib``.

    Read from the library itself: its architecture from the Mach-O header and its minimum
    macOS from LC_BUILD_VERSION (or LC_VERSION_MIN_MACOSX). The build Python is often a
    universal2 binary and its tag says nothing about the Go library inside the wheel: 2.0.16's
    arm64 wheel was tagged universal2, so pip would install it on an Intel Mac.
    """
    data = lib.read_bytes()
    magic, cputype, _subtype, _filetype, ncmds, _size, _flags, _reserved = struct.unpack_from(
        "<8I", data, 0
    )
    if magic != 0xFEEDFACF:
        raise SystemExit(f"{lib}: not a thin 64-bit little-endian Mach-O file (magic {magic:#x})")
    arch = _MACHO_CPU.get(cputype)
    if arch is None:
        raise SystemExit(f"{lib}: unknown Mach-O CPU type {cputype:#x}")
    minos = None
    offset = 32
    for _ in range(ncmds):
        cmd, cmdsize = struct.unpack_from("<2I", data, offset)
        if cmd == _LC_BUILD_VERSION:
            minos = struct.unpack_from("<I", data, offset + 12)[0]
        elif cmd == _LC_VERSION_MIN_MACOSX:
            minos = struct.unpack_from("<I", data, offset + 8)[0]
        offset += cmdsize
    if minos is None:
        raise SystemExit(f"{lib}: no minimum macOS version load command")
    return f"macosx_{minos >> 16}_{(minos >> 8) & 0xFF}_{arch}"


class BinaryBdistWheel(bdist_wheel):
    def get_tag(self):
        _, _, plat = super().get_tag()
        if plat.startswith("macosx_"):
            lib = _PACKAGE_DIR / "libadbc_driver_gizmosql.dylib"
            if lib.is_file():
                plat = _macho_tag(lib=lib)
            elif plat.endswith("_universal2"):
                # No bundled library (an editable install): this machine's architecture.
                plat = plat[: -len("universal2")] + platform.machine()
        return "py3", "none", plat


setup(distclass=BinaryDistribution, cmdclass={"bdist_wheel": BinaryBdistWheel})
