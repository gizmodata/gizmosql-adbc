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

$(LIB): $(wildcard gizmosql/*.go pkg/gizmosql/* pkg/include/arrow-adbc/*)
	go build -tags driverlib -buildmode=c-shared -o $(LIB) ./pkg/gizmosql

test:
	go test ./...

# Build the platform wheel: shared library first, then bundle it inside
# the Python package.
wheel: $(LIB)
	cp $(LIB) ../python/src/adbc_driver_gizmosql/
	cd ../python && python -m build --wheel

clean:
	rm -rf build ../python/src/adbc_driver_gizmosql/libadbc_driver_gizmosql.* ../python/dist
