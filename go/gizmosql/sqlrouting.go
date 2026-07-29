// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"regexp"
	"strings"
)

// sqlKeywords that indicate DDL/DML statements (not SELECT/WITH/SHOW/...).
// When ExecuteQuery sees one of these as the first keyword, it routes
// through ExecuteUpdate (DoPut RPC) for immediate server-side execution.
// Ported verbatim from the 1.x Python driver's _DDL_DML_KEYWORDS.
var ddlDmlKeywords = map[string]struct{}{
	"ALTER":      {},
	"ATTACH":     {},
	"BEGIN":      {},
	"CALL":       {},
	"CHECKPOINT": {},
	"COMMENT":    {},
	"COMMIT":     {},
	"COPY":       {},
	"CREATE":     {},
	"DELETE":     {},
	"DETACH":     {},
	"DROP":       {},
	"EXPORT":     {},
	"GRANT":      {},
	"IMPORT":     {},
	"INSERT":     {},
	"INSTALL":    {},
	"LOAD":       {},
	"MERGE":      {},
	"REVOKE":     {},
	"ROLLBACK":   {},
	"SET":        {},
	"TRUNCATE":   {},
	"UPDATE":     {},
	"USE":        {},
	"VACUUM":     {},
}

var (
	blockCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentRE  = regexp.MustCompile(`--[^\n]*`)
	// SQL doubles the quote character to escape it (''), so the pattern
	// consumes any number of (chars-other-than-quote) or (doubled-quote)
	// before the closing quote.
	singleQuotedRE = regexp.MustCompile(`'(?:[^']|'')*'`)
	doubleQuotedRE = regexp.MustCompile(`"(?:[^"]|"")*"`)
	// Word-boundary RETURNING — DML with a RETURNING clause produces a
	// result set and must NOT take the DoPut/ExecuteUpdate path.
	returningRE = regexp.MustCompile(`(?i)\bRETURNING\b`)
)

// stripSQLComments strips SQL block (/* ... */) and line (-- ...) comments
// and leading whitespace.
func stripSQLComments(sql string) string {
	sql = blockCommentRE.ReplaceAllString(sql, "")
	sql = lineCommentRE.ReplaceAllString(sql, "")
	return strings.TrimLeft(sql, " \t\n\r\v\f")
}

// hasReturningClause reports whether the SQL contains a RETURNING keyword
// outside of string literals. Comments must already be stripped by the
// caller. String literals are blanked first so a value like
// INSERT INTO t VALUES ('returning') is not misclassified.
func hasReturningClause(sql string) bool {
	withoutStrings := singleQuotedRE.ReplaceAllString(sql, "''")
	withoutStrings = doubleQuotedRE.ReplaceAllString(withoutStrings, `""`)
	return returningRE.MatchString(withoutStrings)
}

// isDDLDML reports whether the SQL statement is DDL/DML based on its first
// keyword, after stripping comments (so query-comment prefixes like dbt's
// /* {"app": "dbt"} */ don't mask the statement keyword).
//
// INSERT/UPDATE/DELETE with a RETURNING clause are *not* classified as
// pure DDL/DML: they produce a result set and must take the regular query
// path (with eager materialization) so the returned rows are available.
func isDDLDML(sql string) bool {
	stripped := stripSQLComments(sql)
	if stripped == "" {
		return false
	}
	fields := strings.Fields(stripped)
	firstWord := strings.TrimRight(fields[0], "(;")
	if _, ok := ddlDmlKeywords[strings.ToUpper(firstWord)]; !ok {
		return false
	}
	return !hasReturningClause(stripped)
}
