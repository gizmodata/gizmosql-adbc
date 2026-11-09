// Licensed under the Apache License, Version 2.0.

package gizmosql

import (
	"context"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
)

// The database/connection/statement wrappers embed the base ADBC
// interfaces, which would hide the upstream driver's *optional* option
// interfaces (adbc.PostInitOptions / adbc.GetSetOptions) from type
// assertions — the cgo export layer and driver managers probe for them,
// so hiding them breaks e.g. `adbc.connection.catalog` (used by
// dbt-gizmosql) with "options are not supported". These explicit
// delegations restore the full option surface.

func optionNotSupported(kind, key string) error {
	return adbc.Error{
		Code: adbc.StatusNotImplemented,
		Msg:  "[GizmoSQL] " + kind + " does not support option " + strconv.Quote(key),
	}
}

// --- database ---

var _ adbc.GetSetOptions = (*database)(nil)

func (db *database) SetOption(key, value string) error {
	if o, ok := db.Database.(adbc.PostInitOptions); ok {
		return o.SetOption(key, value)
	}
	return optionNotSupported("database", key)
}

func (db *database) SetOptionBytes(key string, value []byte) error {
	if o, ok := db.Database.(adbc.GetSetOptions); ok {
		return o.SetOptionBytes(key, value)
	}
	return optionNotSupported("database", key)
}

func (db *database) SetOptionInt(key string, value int64) error {
	if o, ok := db.Database.(adbc.GetSetOptions); ok {
		return o.SetOptionInt(key, value)
	}
	return optionNotSupported("database", key)
}

func (db *database) SetOptionDouble(key string, value float64) error {
	if o, ok := db.Database.(adbc.GetSetOptions); ok {
		return o.SetOptionDouble(key, value)
	}
	return optionNotSupported("database", key)
}

func (db *database) GetOption(key string) (string, error) {
	if o, ok := db.Database.(adbc.GetSetOptions); ok {
		return o.GetOption(key)
	}
	return "", optionNotSupported("database", key)
}

func (db *database) GetOptionBytes(key string) ([]byte, error) {
	if o, ok := db.Database.(adbc.GetSetOptions); ok {
		return o.GetOptionBytes(key)
	}
	return nil, optionNotSupported("database", key)
}

func (db *database) GetOptionInt(key string) (int64, error) {
	if o, ok := db.Database.(adbc.GetSetOptions); ok {
		return o.GetOptionInt(key)
	}
	return 0, optionNotSupported("database", key)
}

func (db *database) GetOptionDouble(key string) (float64, error) {
	if o, ok := db.Database.(adbc.GetSetOptions); ok {
		return o.GetOptionDouble(key)
	}
	return 0, optionNotSupported("database", key)
}

// --- connection ---

var _ adbc.GetSetOptions = (*connection)(nil)

func (c *connection) SetOption(key, value string) error {
	if o, ok := c.Connection.(adbc.PostInitOptions); ok {
		return o.SetOption(key, value)
	}
	return optionNotSupported("connection", key)
}

func (c *connection) SetOptionBytes(key string, value []byte) error {
	if o, ok := c.Connection.(adbc.GetSetOptions); ok {
		return o.SetOptionBytes(key, value)
	}
	return optionNotSupported("connection", key)
}

func (c *connection) SetOptionInt(key string, value int64) error {
	if o, ok := c.Connection.(adbc.GetSetOptions); ok {
		return o.SetOptionInt(key, value)
	}
	return optionNotSupported("connection", key)
}

func (c *connection) SetOptionDouble(key string, value float64) error {
	if o, ok := c.Connection.(adbc.GetSetOptions); ok {
		return o.SetOptionDouble(key, value)
	}
	return optionNotSupported("connection", key)
}

func (c *connection) GetOption(key string) (string, error) {
	if o, ok := c.Connection.(adbc.GetSetOptions); ok {
		return o.GetOption(key)
	}
	return "", optionNotSupported("connection", key)
}

func (c *connection) GetOptionBytes(key string) ([]byte, error) {
	if o, ok := c.Connection.(adbc.GetSetOptions); ok {
		return o.GetOptionBytes(key)
	}
	return nil, optionNotSupported("connection", key)
}

func (c *connection) GetOptionInt(key string) (int64, error) {
	if o, ok := c.Connection.(adbc.GetSetOptions); ok {
		return o.GetOptionInt(key)
	}
	return 0, optionNotSupported("connection", key)
}

func (c *connection) GetOptionDouble(key string) (float64, error) {
	if o, ok := c.Connection.(adbc.GetSetOptions); ok {
		return o.GetOptionDouble(key)
	}
	return 0, optionNotSupported("connection", key)
}

// --- statement ---
// SetOption is part of the base adbc.Statement interface (already
// promoted via embedding); the remaining GetSetOptions methods need
// explicit delegation.

var _ adbc.GetSetOptions = (*statement)(nil)

func (s *statement) SetOptionBytes(key string, value []byte) error {
	if o, ok := s.Statement.(adbc.GetSetOptions); ok {
		if err := o.SetOptionBytes(key, value); err != nil {
			return err
		}
		s.recordOption(key, value)
		return nil
	}
	return optionNotSupported("statement", key)
}

func (s *statement) SetOptionInt(key string, value int64) error {
	if o, ok := s.Statement.(adbc.GetSetOptions); ok {
		if err := o.SetOptionInt(key, value); err != nil {
			return err
		}
		s.recordOption(key, value)
		return nil
	}
	return optionNotSupported("statement", key)
}

func (s *statement) SetOptionDouble(key string, value float64) error {
	if o, ok := s.Statement.(adbc.GetSetOptions); ok {
		if err := o.SetOptionDouble(key, value); err != nil {
			return err
		}
		s.recordOption(key, value)
		return nil
	}
	return optionNotSupported("statement", key)
}

func (s *statement) GetOption(key string) (string, error) {
	if o, ok := s.Statement.(adbc.GetSetOptions); ok {
		return o.GetOption(key)
	}
	return "", optionNotSupported("statement", key)
}

func (s *statement) GetOptionBytes(key string) ([]byte, error) {
	if o, ok := s.Statement.(adbc.GetSetOptions); ok {
		return o.GetOptionBytes(key)
	}
	return nil, optionNotSupported("statement", key)
}

func (s *statement) GetOptionInt(key string) (int64, error) {
	if o, ok := s.Statement.(adbc.GetSetOptions); ok {
		return o.GetOptionInt(key)
	}
	return 0, optionNotSupported("statement", key)
}

func (s *statement) GetOptionDouble(key string) (float64, error) {
	if o, ok := s.Statement.(adbc.GetSetOptions); ok {
		return o.GetOptionDouble(key)
	}
	return 0, optionNotSupported("statement", key)
}

// --- optional statement interfaces ---

var _ adbc.StatementExecuteSchema = (*statement)(nil)

// ExecuteSchema forwards to the upstream statement. Like the option
// interfaces above, adbc.StatementExecuteSchema is optional and so hidden by
// the embedded adbc.Statement: without this forwarder the C library answered
// every AdbcStatementExecuteSchema with NOT_IMPLEMENTED although upstream
// (and GizmoSQL) support it.
//
// GizmoSQL answers GetSchema only for prepared statements (an unprepared
// GetSchema(CommandStatementQuery) is "GetSchemaStatement not implemented"),
// so an unprepared SQL query is prepared first; preparing plans the query
// without running it, and the prepared statement then carries the schema.
func (s *statement) ExecuteSchema(ctx context.Context) (*arrow.Schema, error) {
	es, ok := s.Statement.(adbc.StatementExecuteSchema)
	if !ok {
		return nil, adbc.Error{Code: adbc.StatusNotImplemented, Msg: "[GizmoSQL] ExecuteSchema is not supported"}
	}
	if s.query != "" && s.ingestTarget == "" && !s.prepared {
		if err := s.Prepare(ctx); err != nil {
			return nil, err
		}
	}
	return es.ExecuteSchema(ctx)
}
