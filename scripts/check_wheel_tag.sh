#!/usr/bin/env bash
# Check that a built wheel's platform tag describes the library inside it.
#
#   scripts/check_wheel_tag.sh <expected platform tag (a shell glob)> <wheel>
#
# The filename must end in -py3-none-<expected>.whl. For a macOS wheel, the bundled dylib's
# architecture (lipo) and minimum macOS (otool -l, LC_BUILD_VERSION minos) must also equal the
# tag's: 2.0.16 shipped an arm64-only dylib in a wheel tagged universal2.
set -euo pipefail
[ $# -eq 2 ] || { echo "usage: $0 <expected tag glob> <wheel>" >&2; exit 2; }
expected=$1
wheel=$2
name=$(basename "$wheel")

# shellcheck disable=SC2254  # $expected is a glob on purpose
case "$name" in
  adbc_driver_gizmosql-*-py3-none-$expected.whl) ;;
  *) echo "::error::$name: platform tag is not $expected" >&2; exit 1 ;;
esac

case "$name" in
  *-macosx_*)
    tag=${name##*-py3-none-}
    tag=${tag%.whl}                           # macosx_12_0_arm64
    arch=${tag#macosx_*_*_}                   # arm64
    ver=${tag#macosx_}
    ver=${ver%_"$arch"}                       # 12_0
    ver=${ver/_/.}                            # 12.0
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    python3 -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' "$wheel" "$tmp"
    lib="$tmp/adbc_driver_gizmosql/libadbc_driver_gizmosql.dylib"
    [ -f "$lib" ] || { echo "::error::$name: no libadbc_driver_gizmosql.dylib inside" >&2; exit 1; }
    archs=$(lipo -archs "$lib")
    minos=$(otool -l "$lib" | awk '/LC_BUILD_VERSION/ {b=1} b && $1 == "minos" {print $2; exit}')
    if [ "$archs" != "$arch" ]; then
      echo "::error::$name: tag says $arch, the dylib is '$archs'" >&2; exit 1
    fi
    if [ "$minos" != "$ver" ]; then
      echo "::error::$name: tag says macOS $ver, the dylib's minos is '$minos'" >&2; exit 1
    fi
    echo "$name: dylib is $archs, minos $minos"
    ;;
esac
echo "$name: platform tag matches $expected"
