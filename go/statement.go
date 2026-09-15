// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
)

// Statement is to implement Go's database/sql Statement.
type Statement struct {
	connection *Connection
	closed     bool
	query      string
}

// Close is to close an open statement. Idempotent per driver.Stmt
// contract (see golang/go#16019); returning ErrBadConn on the second
// call causes database/sql to evict the underlying connection.
func (s *Statement) Close() error {
	s.closed = true
	return nil
}

// NumInput returns -1 so database/sql skips its own arg-count check.
// strings.Count on `?` is quote-blind and would reject a literal `?` inside
// a string. Athena validates ExecutionParameters count server-side;
// interpolateParams does its own quote-aware count on the DDL path.
func (s *Statement) NumInput() int {
	return -1
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
