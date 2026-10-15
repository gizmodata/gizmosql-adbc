#!/usr/bin/env bash
# Build the driver's C shared library (go/build/libadbc_driver_gizmosql.<ext>).
#
# On Linux the C toolchain is `zig cc` targeting glibc 2.17, so the library (and the wheel that
# auditwheel tags from it) runs on any glibc >= 2.17: Oracle Linux / RHEL 7 and 8, Amazon Linux,
# Debian 10+, ... Built with the runner's own gcc it would need the runner's glibc (2.34+ on
# ubuntu-latest), which made the wheels uninstallable on RHEL-family 8 servers.
set -euo pipefail
cd "$(dirname "$0")/../go"

case "$(go env GOOS)" in
  windows) EXT=dll ;;
  darwin)  EXT=dylib ;;
  *)       EXT=so ;;
esac

if [ "$(go env GOOS)" = "linux" ]; then
  case "$(go env GOARCH)" in
    amd64) TARGET=x86_64-linux-gnu.2.17 ;;
    arm64) TARGET=aarch64-linux-gnu.2.17 ;;
    *) echo "unsupported GOARCH $(go env GOARCH)" >&2; exit 1 ;;
  esac
  command -v zig >/dev/null || { echo "zig is required to build the Linux library" >&2; exit 1; }
  export CC="zig cc -target $TARGET"
fi

CGO_ENABLED=1 go build -tags driverlib -buildmode=c-shared -o "build/libadbc_driver_gizmosql.$EXT" ./pkg/gizmosql
ls -la build/
