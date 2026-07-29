"""Locate the bundled Go GizmoSQL ADBC driver shared library."""

from __future__ import annotations

import functools
import os
import pathlib
import sys


@functools.cache
def _driver_path() -> str:
    """Return the path to libadbc_driver_gizmosql for this platform.

    Search order:
      1. ``GIZMOSQL_DRIVER_LIB`` environment variable (explicit override)
      2. the shared library bundled inside this package (wheel installs)
      3. the repo-layout build output ``go/build/`` (development checkouts)
    """
    if env := os.environ.get("GIZMOSQL_DRIVER_LIB"):
        return env

    if sys.platform == "darwin":
        exts = ("dylib",)
    elif os.name == "nt":
        exts = ("dll",)
    else:
        exts = ("so",)

    pkg_dir = pathlib.Path(__file__).resolve().parent
    candidates = [pkg_dir] + [
        pkg_dir.parent.parent.parent / "go" / "build",  # python/src/../../go/build
    ]
    for directory in candidates:
        for ext in exts:
            lib = directory / f"libadbc_driver_gizmosql.{ext}"
            if lib.exists():
                return str(lib)

    raise RuntimeError(
        "libadbc_driver_gizmosql shared library not found. Install a "
        "platform wheel of adbc-driver-gizmosql, set GIZMOSQL_DRIVER_LIB, "
        "or build it from source with `make -C go lib`."
    )
