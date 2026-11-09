// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// infoReader builds a GetInfo result like the upstream driver's.
func infoReader(t *testing.T) array.RecordReader {
	t.Helper()
	bldr := array.NewRecordBuilder(memory.DefaultAllocator, adbc.GetInfoSchema)
	defer bldr.Release()
	codes := bldr.Field(0).(*array.Uint32Builder)
	vals := bldr.Field(1).(*array.DenseUnionBuilder)
	str := vals.Child(int(adbc.InfoValueStringType)).(*array.StringBuilder)
	i64 := vals.Child(int(adbc.InfoValueInt64Type)).(*array.Int64Builder)
	boolean := vals.Child(int(adbc.InfoValueBooleanType)).(*array.BooleanBuilder)

	addStr := func(code adbc.InfoCode, v string, valid bool) {
		codes.Append(uint32(code))
		vals.Append(adbc.InfoValueStringType)
		if valid {
			str.Append(v)
		} else {
			str.AppendNull()
		}
	}
	addStr(adbc.InfoVendorName, "gizmosql", true)
	addStr(adbc.InfoDriverName, "ADBC Flight SQL Driver - Go", true)
	addStr(adbc.InfoDriverVersion, "(unknown or development build)", true)
	addStr(adbc.InfoVendorArrowVersion, "", false)
	codes.Append(uint32(adbc.InfoDriverADBCVersion))
	vals.Append(adbc.InfoValueInt64Type)
	i64.Append(adbc.AdbcVersion1_1_0)
	codes.Append(uint32(adbc.InfoVendorSql))
	vals.Append(adbc.InfoValueBooleanType)
	boolean.Append(true)

	rec := bldr.NewRecordBatch()
	defer rec.Release()
	rdr, err := array.NewRecordReader(adbc.GetInfoSchema, []arrow.RecordBatch{rec})
	if err != nil {
		t.Fatal(err)
	}
	return rdr
}

func TestRewriteInfo(t *testing.T) {
	saved := DriverVersion
	DriverVersion = "v9.8.7"
	defer func() { DriverVersion = saved }()

	out, err := rewriteInfo(memory.DefaultAllocator, infoReader(t))
	if err != nil {
		t.Fatal(err)
	}
	got := readInfo(t, out)
	want := map[adbc.InfoCode]any{
		adbc.InfoVendorName:         "gizmosql",
		adbc.InfoDriverName:         DriverName,
		adbc.InfoDriverVersion:      "v9.8.7",
		adbc.InfoVendorArrowVersion: nil,
		adbc.InfoDriverADBCVersion:  int64(adbc.AdbcVersion1_1_0),
		adbc.InfoVendorSql:          true,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d info codes, want %d: %v", len(got), len(want), got)
	}
	for code, w := range want {
		if got[code] != w {
			t.Errorf("info %d = %#v, want %#v", code, got[code], w)
		}
	}
}

func TestDriverVersionDevelopmentBuild(t *testing.T) {
	saved := DriverVersion
	DriverVersion = ""
	defer func() { DriverVersion = saved }()
	// The test binary's main module is this module, not a dependency.
	if got := driverVersion(); got != "unknown" {
		t.Errorf("driverVersion() = %q, want unknown", got)
	}
}
