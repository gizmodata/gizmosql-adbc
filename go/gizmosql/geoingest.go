// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Geometry-aware bulk ingest (gizmodata/adbc-driver-gizmosql#5).
//
// GizmoSQL serves geometry columns as Arrow binary fields tagged with the
// `geoarrow.wkb` extension metadata, but the plain Flight SQL ingest path
// ignores extension metadata: create-mode ingests produce BLOB columns,
// and append-mode ingests into a GEOMETRY column fail server-side with a
// blob→geometry conversion error.
//
// When the bound data contains geoarrow.* columns, the driver reroutes
// the ingest through a session-temporary interim table:
//
//  1. bulk-ingest the data into the interim table (columns land as BLOB)
//  2. ALTER each geo column to GEOMETRY USING ST_GeomFromWKB(col)
//  3. materialize into the real target per the requested ingest mode
//  4. drop the interim table
//
// Non-geometry ingests are delegated untouched.

const arrowExtensionNameKey = "ARROW:extension:name"

// geoFieldNames returns the names of schema fields tagged with a
// geoarrow.* Arrow extension.
func geoFieldNames(schema *arrow.Schema) []string {
	if schema == nil {
		return nil
	}
	var names []string
	for _, f := range schema.Fields() {
		if v, ok := f.Metadata.GetValue(arrowExtensionNameKey); ok &&
			strings.HasPrefix(v, "geoarrow.") {
			names = append(names, f.Name)
		}
	}
	return names
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// qualifiedTarget builds the (optionally catalog/schema-qualified) target
// table reference. Temporary targets are never qualified.
func (s *statement) qualifiedTarget() string {
	if s.ingestTemp {
		return quoteIdent(s.ingestTarget)
	}
	var parts []string
	if s.ingestCatalog != "" {
		parts = append(parts, quoteIdent(s.ingestCatalog))
	}
	if s.ingestDBSchema != "" {
		parts = append(parts, quoteIdent(s.ingestDBSchema))
	}
	parts = append(parts, quoteIdent(s.ingestTarget))
	return strings.Join(parts, ".")
}

// execSQL runs one eager DDL/DML statement on a fresh inner statement.
func execSQL(ctx context.Context, cnxn adbc.Connection, sql string) error {
	st, err := cnxn.NewStatement()
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.SetSqlQuery(sql); err != nil {
		return err
	}
	_, err = st.ExecuteUpdate(ctx)
	return err
}

// executeGeoIngest performs the interim-table ingest dance described in
// the file comment. Returns the ingested row count.
func (s *statement) executeGeoIngest(ctx context.Context, geoCols []string) (int64, error) {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return -1, err
	}
	interim := "gizmosql_geo_interim_" + hex.EncodeToString(suffix)

	// Redirect the inner statement's ingest at a session-temporary
	// interim table. (The wrapper recreates the inner statement on the
	// next SetSqlQuery, so the redirected options never leak.)
	for _, kv := range [][2]string{
		{adbc.OptionKeyIngestTargetTable, interim},
		{adbc.OptionKeyIngestMode, adbc.OptionValueIngestModeReplace},
		{adbc.OptionValueIngestTemporary, "true"},
		{adbc.OptionValueIngestTargetCatalog, ""},
		{adbc.OptionValueIngestTargetDBSchema, ""},
	} {
		if err := s.Statement.SetOption(kv[0], kv[1]); err != nil {
			return -1, err
		}
	}

	affected, err := s.Statement.ExecuteUpdate(ctx)
	if err != nil {
		return -1, err
	}

	dropInterim := func() {
		_ = execSQL(ctx, s.cnxn, "DROP TABLE IF EXISTS "+quoteIdent(interim))
	}

	// Restore the geometry type on the interim table — but only for columns
	// that actually landed as BLOB. GizmoSQL >= 1.37.0 honours the geoarrow.*
	// extension metadata itself and creates GEOMETRY columns directly, in
	// which case ST_GeomFromWKB(GEOMETRY) would not bind; older servers
	// ignore the metadata and leave BLOB. Checking the resulting type keeps
	// this path correct against both without sniffing server versions.
	blobCols, err := blobTypedColumns(ctx, s.cnxn, interim, geoCols)
	if err != nil {
		dropInterim()
		return -1, wrapGeoErr("inspecting interim column types", err)
	}
	for _, col := range blobCols {
		q := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE GEOMETRY USING ST_GeomFromWKB(%s)",
			quoteIdent(interim), quoteIdent(col), quoteIdent(col))
		if err := execSQL(ctx, s.cnxn, q); err != nil {
			dropInterim()
			return -1, wrapGeoErr("restoring GEOMETRY type", err)
		}
	}

	// Materialize into the real target per the requested mode.
	target := s.qualifiedTarget()
	tempKW := ""
	if s.ingestTemp {
		tempKW = "TEMP "
	}
	var stmts []string
	switch s.ingestMode {
	case adbc.OptionValueIngestModeReplace:
		stmts = []string{fmt.Sprintf("CREATE OR REPLACE %sTABLE %s AS SELECT * FROM %s",
			tempKW, target, quoteIdent(interim))}
	case adbc.OptionValueIngestModeAppend:
		stmts = []string{fmt.Sprintf("INSERT INTO %s BY NAME SELECT * FROM %s",
			target, quoteIdent(interim))}
	case adbc.OptionValueIngestModeCreateAppend:
		stmts = []string{
			fmt.Sprintf("CREATE %sTABLE IF NOT EXISTS %s AS SELECT * FROM %s LIMIT 0",
				tempKW, target, quoteIdent(interim)),
			fmt.Sprintf("INSERT INTO %s BY NAME SELECT * FROM %s",
				target, quoteIdent(interim)),
		}
	default: // create (the ADBC default when no mode was set)
		stmts = []string{fmt.Sprintf("CREATE %sTABLE %s AS SELECT * FROM %s",
			tempKW, target, quoteIdent(interim))}
	}
	for _, q := range stmts {
		if err := execSQL(ctx, s.cnxn, q); err != nil {
			dropInterim()
			return -1, wrapGeoErr("materializing ingest target", err)
		}
	}
	dropInterim()
	return affected, nil
}

// blobTypedColumns returns the subset of `cols` whose type on `table` is
// BLOB, i.e. geo columns the server did not already materialize as GEOMETRY.
func blobTypedColumns(ctx context.Context, cnxn adbc.Connection, table string, cols []string) ([]string, error) {
	st, err := cnxn.NewStatement()
	if err != nil {
		return nil, err
	}
	defer st.Close()
	q := fmt.Sprintf(
		"SELECT column_name FROM duckdb_columns() WHERE table_name = %s AND upper(data_type) = 'BLOB'",
		quoteLiteral(table))
	if err := st.SetSqlQuery(q); err != nil {
		return nil, err
	}
	reader, _, err := st.ExecuteQuery(ctx)
	if err != nil {
		return nil, err
	}
	defer reader.Release()
	want := make(map[string]bool, len(cols))
	for _, c := range cols {
		want[c] = true
	}
	var out []string
	for reader.Next() {
		rec := reader.Record()
		names, ok := rec.Column(0).(*array.String)
		if !ok {
			return nil, fmt.Errorf("unexpected column_name type %s", rec.Column(0).DataType())
		}
		for i := 0; i < names.Len(); i++ {
			if n := names.Value(i); want[n] {
				out = append(out, n)
			}
		}
	}
	return out, reader.Err()
}

// quoteLiteral renders s as a single-quoted SQL string literal.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func wrapGeoErr(stage string, err error) error {
	if ae, ok := err.(adbc.Error); ok {
		ae.Msg = "[GizmoSQL] geometry-aware ingest: " + stage + ": " + ae.Msg
		return ae
	}
	return fmt.Errorf("[GizmoSQL] geometry-aware ingest: %s: %w", stage, err)
}
