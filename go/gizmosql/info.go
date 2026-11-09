// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"fmt"
	"runtime/debug"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// DriverName is reported as ADBC_INFO_DRIVER_NAME.
const DriverName = "ADBC GizmoSQL Driver - Go"

const modulePath = "github.com/gizmodata/gizmosql-adbc/go"

// DriverVersion is reported as ADBC_INFO_DRIVER_VERSION. Release builds of
// the shared library set it to the release tag with
//
//	-ldflags "-X github.com/gizmodata/gizmosql-adbc/go/gizmosql.DriverVersion=vX.Y.Z"
//
// (scripts/build_lib.sh, DRIVER_VERSION). When it is empty, a program that
// imports this package as a Go module reports the module version it
// resolved, and anything else (a development build) reports "unknown".
var DriverVersion = ""

func driverVersion() string {
	if DriverVersion != "" {
		return DriverVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == modulePath && dep.Version != "" && dep.Version != "(devel)" {
				return dep.Version
			}
		}
	}
	return "unknown"
}

// rewriteInfo copies a GetInfo result, replacing the driver name and version.
// The upstream Flight SQL driver reports itself ("ADBC Flight SQL Driver - Go")
// with an unknown version, because its version comes from its own module's
// build info, which a library built from this repository does not carry.
func rewriteInfo(alloc memory.Allocator, in array.RecordReader) (array.RecordReader, error) {
	defer in.Release()
	overrides := map[adbc.InfoCode]string{
		adbc.InfoDriverName:    DriverName,
		adbc.InfoDriverVersion: driverVersion(),
	}

	bldr := array.NewRecordBuilder(alloc, adbc.GetInfoSchema)
	defer bldr.Release()
	codeBldr := bldr.Field(0).(*array.Uint32Builder)
	valueBldr := bldr.Field(1).(*array.DenseUnionBuilder)

	for in.Next() {
		rec := in.RecordBatch()
		codes, ok := rec.Column(0).(*array.Uint32)
		if !ok {
			return nil, fmt.Errorf("[GizmoSQL] GetInfo: unexpected info_name column %T", rec.Column(0))
		}
		values, ok := rec.Column(1).(*array.DenseUnion)
		if !ok {
			return nil, fmt.Errorf("[GizmoSQL] GetInfo: unexpected info_value column %T", rec.Column(1))
		}
		for i := 0; i < int(rec.NumRows()); i++ {
			code := adbc.InfoCode(codes.Value(i))
			codeBldr.Append(uint32(code))
			if v, ok := overrides[code]; ok {
				valueBldr.Append(adbc.InfoValueStringType)
				valueBldr.Child(int(adbc.InfoValueStringType)).(*array.StringBuilder).Append(v)
				continue
			}
			if err := appendUnionValue(valueBldr, values, i); err != nil {
				return nil, err
			}
		}
	}
	if err := in.Err(); err != nil {
		return nil, err
	}

	rec := bldr.NewRecordBatch()
	defer rec.Release()
	return array.NewRecordReader(adbc.GetInfoSchema, []arrow.RecordBatch{rec})
}

// appendUnionValue copies row i of a GetInfo dense union into bldr.
func appendUnionValue(bldr *array.DenseUnionBuilder, values *array.DenseUnion, i int) error {
	typeCode := values.TypeCode(i)
	child := values.Field(values.ChildID(i))
	idx := int(values.ValueOffset(i))
	bldr.Append(typeCode)
	cb := bldr.Child(values.ChildID(i))
	if child.IsNull(idx) {
		cb.AppendNull()
		return nil
	}
	switch c := child.(type) {
	case *array.String:
		cb.(*array.StringBuilder).Append(c.Value(idx))
	case *array.Boolean:
		cb.(*array.BooleanBuilder).Append(c.Value(idx))
	case *array.Int64:
		cb.(*array.Int64Builder).Append(c.Value(idx))
	case *array.Int32:
		cb.(*array.Int32Builder).Append(c.Value(idx))
	default:
		// string_list / int32_to_int32_list_map: copy through the
		// builder's JSON round trip.
		if err := cb.AppendValueFromString(child.ValueStr(idx)); err != nil {
			return fmt.Errorf("[GizmoSQL] GetInfo: copying %s value: %w", child.DataType(), err)
		}
	}
	return nil
}
