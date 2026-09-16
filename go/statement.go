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

// Close is to close an open statement. Idempotent per driver.Stmt
// contract (see golang/go#16019); returning ErrBadConn on the second
// call causes database/sql to evict the underlying connection.
func (s *Statement) Close() error {
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

// Exec is to execute a prepared statement.
func (s *Statement) Exec(args []driver.Value) (driver.Result, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	return s.connection.ExecContext(context.Background(), s.query,
		valueToNamedValue(args))
}

// Query is to query based on a prepared statement.
func (s *Statement) Query(args []driver.Value) (driver.Rows, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	return s.connection.QueryContext(context.Background(), s.query,
		valueToNamedValue(args))
}

// ExecContext implements driver.StmtExecContext. When the parent Stmt
// implements this, database/sql prefers it over the ctx-less Exec, so a
// caller's context cancellation reaches StartQueryExecution and the polling
// loop.
func (s *Statement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	return s.connection.ExecContext(ctx, s.query, args)
}

// QueryContext implements driver.StmtQueryContext.
func (s *Statement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if s.closed {
		return nil, driver.ErrBadConn
	}
	return s.connection.QueryContext(ctx, s.query, args)
}

var _ driver.StmtExecContext = (*Statement)(nil)
var _ driver.StmtQueryContext = (*Statement)(nil)
