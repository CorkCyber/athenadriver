// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"strings"
)

// Statement is to implement Go's database/sql Statement.
type Statement struct {
	connection *Connection
	closed     bool
	query      string
	numInput   int
}

// Close is to close an open statement.
func (s *Statement) Close() error {
	if s.connection == nil || s.closed {
		// driver.Stmt.Close can be called more than once, thus this function
		// has to be idempotent.
		// See also Issue #450 and golang/go#16019.
		return driver.ErrBadConn
	}
	s.query = ""
	s.closed = true
	s.numInput = 0
	return nil
}

// NumInput returns the number of prepared arguments.
// It may also return -1, if the driver doesn't know
// its number of placeholders. In that case, the sql package
// will not sanity check Exec or Query argument counts.
// -- From Go `sql/driver`
func (s *Statement) NumInput() int {
	if s.numInput == 0 {
		s.numInput = strings.Count(s.query, "?")
	}
	return s.numInput
}

// ColumnConverter is to return driver's DefaultParameterConverter.
func (s *Statement) ColumnConverter(idx int) driver.ValueConverter {
	return driver.DefaultParameterConverter
}

// Exec is to execute a prepared statement.
func (s *Statement) Exec(args []driver.Value) (driver.Result, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	r, e := s.connection.ExecContext(context.Background(), s.query,
		valueToNamedValue(args))
	s.closed = true
	return r, e
}

// Query is to query based on a prepared statement.
func (s *Statement) Query(args []driver.Value) (driver.Rows, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	r, e := s.connection.QueryContext(context.Background(), s.query,
		valueToNamedValue(args))
	s.closed = true
	return r, e
}

// ExecContext implements driver.StmtExecContext. When the parent Stmt
// implements this, database/sql prefers it over the ctx-less Exec, so a
// caller's context cancellation reaches StartQueryExecution and the polling
// loop.
func (s *Statement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	r, e := s.connection.ExecContext(ctx, s.query, args)
	s.closed = true
	return r, e
}

// QueryContext implements driver.StmtQueryContext.
func (s *Statement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	r, e := s.connection.QueryContext(ctx, s.query, args)
	s.closed = true
	return r, e
}

var _ driver.StmtExecContext = (*Statement)(nil)
var _ driver.StmtQueryContext = (*Statement)(nil)
