# Licensed under the Apache License, Version 2.0.
#
# Build libadbc_driver_gizmosql as a C shared library via cgo.

GOOS := $(shell go env GOOS)

ifeq ($(GOOS),linux)
	SUFFIX = so
else ifeq ($(GOOS),windows)
	SUFFIX = dll
else
	SUFFIX = dylib
endif

LIB = build/libadbc_driver_gizmosql.$(SUFFIX)

.PHONY: all lib test clean wheel

all: lib

lib: $(LIB)

# scripts/build_lib.sh sets the export lists, the Linux glibc 2.17 toolchain (zig cc) and the
# macOS deployment target, and checks the library's exports.
$(LIB): $(wildcard gizmosql/*.go pkg/gizmosql/* pkg/include/arrow-adbc/* internal/checkexports/*.go ../scripts/build_lib.sh)
	../scripts/build_lib.sh

test:
	go test ./...

# Build the platform wheel: shared library first, then bundle it inside
# the Python package.
wheel: $(LIB)
	python3 ../scripts/sync_pypi_readme.py
	cp $(LIB) ../python/src/adbc_driver_gizmosql/
	cd ../python && python -m build --wheel

clean:
	rm -rf build ../python/src/adbc_driver_gizmosql/libadbc_driver_gizmosql.* ../python/dist
