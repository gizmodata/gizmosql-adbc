#!/usr/bin/env bash
# Build the driver's C shared library (go/build/libadbc_driver_gizmosql.<ext>) for the target in
# GOOS/GOARCH (default: this machine), then check its exports. Every build of the library (the
# Makefile, CI's test jobs, the release) goes through this script.
#
# On Linux the C toolchain is `zig cc` targeting glibc 2.17, so the library (and the wheel that
# auditwheel tags from it) runs on any glibc >= 2.17: Oracle Linux / RHEL 7 and 8, Amazon Linux,
# Debian 10+, ... Built with the runner's own gcc it would need the runner's glibc (2.34+ on
# ubuntu-latest), which made the wheels uninstallable on RHEL-family 8 servers.
#
# The library exports only the ADBC entrypoints (Adbc*) plus the deprecated GizmoSQLDriverInit
# and GizmoSqlDriverInit aliases (to be removed in 3.0), per the ADBC shared-library requirements
# (https://adbc-drivers.org/building-drivers/shared-library-requirements.html). The export lists
# live next to the package in go/pkg/gizmosql: exports_linux.map (version script),
# exports_darwin.txt (ld64 exported-symbols list) and exports_windows.def. On Windows,
# -Wl,--export-all-symbols makes Go skip its own .def file (which lists every //export function,
# e.g. GizmoSQLDatabaseNew), and --exclude-all-symbols then turns automatic exporting off again,
# so the DLL exports what the C code marks dllexport (the Adbc* wrappers in utils.c) plus our
# .def. `go run ./internal/checkexports` (run below) verifies the result.
#
# On macOS the library's minimum OS (LC_BUILD_VERSION minos) is MACOSX_DEPLOYMENT_TARGET
# (default 12.0, the oldest macOS Go 1.26 supports). It is passed to the C compiler and linker
# explicitly, because Go's build cache does not key on MACOSX_DEPLOYMENT_TARGET.
#
# CC, CGO_CFLAGS, CGO_LDFLAGS and the like pass through to go build.
set -euo pipefail
cd "$(dirname "$0")/../go"

goos=$(go env GOOS)
case "$goos" in
  windows) EXT=dll ;;
  darwin)  EXT=dylib ;;
  linux)   EXT=so ;;
  *) echo "build_lib.sh: unsupported GOOS $goos" >&2; exit 1 ;;
esac

if [ "$goos" = "linux" ]; then
  case "$(go env GOARCH)" in
    amd64) TARGET=x86_64-linux-gnu.2.17 ;;
    arm64) TARGET=aarch64-linux-gnu.2.17 ;;
    *) echo "unsupported GOARCH $(go env GOARCH)" >&2; exit 1 ;;
  esac
  command -v zig >/dev/null || { echo "zig is required to build the Linux library" >&2; exit 1; }
  export CC="zig cc -target $TARGET"
fi

if [ "$goos" = "darwin" ]; then
  export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"
  # Setting CGO_CFLAGS at all replaces Go's "-O2 -g" default, so keep it.
  export CGO_CFLAGS="${CGO_CFLAGS:--O2 -g} -mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"
  export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=$MACOSX_DEPLOYMENT_TARGET"
fi

# The external linker runs in a temporary directory: give it absolute paths, in the native form
# on Windows (D:/a/..., not Git Bash's /d/a/...).
dir=$PWD/pkg/gizmosql
if command -v cygpath >/dev/null 2>&1; then
  dir=$(cygpath -m "$dir")
fi
case "$goos" in
  linux)   EXTLD="-Wl,--version-script=$dir/exports_linux.map" ;;
  darwin)  EXTLD="-Wl,-exported_symbols_list,$dir/exports_darwin.txt" ;;
  windows) EXTLD="-Wl,--export-all-symbols -Wl,--exclude-all-symbols $dir/exports_windows.def" ;;
esac

# DRIVER_VERSION (e.g. v2.0.17, set by release.yml on tags) is reported as
# ADBC_INFO_DRIVER_VERSION; unset, the library reports "unknown".
LDFLAGS="-extldflags '$EXTLD'"
if [ -n "${DRIVER_VERSION:-}" ]; then
  LDFLAGS="-X github.com/gizmodata/gizmosql-adbc/go/gizmosql.DriverVersion=${DRIVER_VERSION} $LDFLAGS"
fi

LIB="build/libadbc_driver_gizmosql.$EXT"
CGO_ENABLED=1 go build -tags driverlib -buildmode=c-shared -ldflags "$LDFLAGS" -o "$LIB" ./pkg/gizmosql
ls -la build/

# The checker runs on the build machine (not GOOS/GOARCH) and reads ELF, Mach-O and PE alike.
GOOS= GOARCH= CGO_ENABLED=0 go run ./internal/checkexports \
  -allow GizmoSQLDriverInit,GizmoSqlDriverInit "$LIB"
