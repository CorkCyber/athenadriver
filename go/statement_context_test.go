// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestStatement(t *testing.T, query string) *Statement {
	t.Helper()
	cfg := NewNoOpsConfig()
	connector := &SQLConnector{config: cfg}
	conn, err := connector.Connect(context.Background())
	assert.NoError(t, err)
	return &Statement{
		connection: conn.(*Connection),
		query:      query,
	}
}

// A closed Statement must reject *Context calls with driver.ErrBadConn, the
// same signal database/sql expects to trigger connection eviction.

func TestStatement_ExecContext_ClosedReturnsBadConn(t *testing.T) {
	st := newTestStatement(t, "insert into t values (?)")
	assert.NoError(t, st.Close())
	_, err := st.ExecContext(context.Background(), []driver.NamedValue{{Ordinal: 1, Value: "x"}})
	assert.Equal(t, driver.ErrBadConn, err)
}

func TestStatement_QueryContext_ClosedReturnsBadConn(t *testing.T) {
	st := newTestStatement(t, "select 1")
	assert.NoError(t, st.Close())
	_, err := st.QueryContext(context.Background(), nil)
	assert.Equal(t, driver.ErrBadConn, err)
}

// An open Statement routes ExecContext / QueryContext through its Connection.
// With the NoOps config the underlying Athena client is nil, so these assert
// on the error path (no real Athena roundtrip), just verifying the
// driver.StmtExecContext / StmtQueryContext hook paths are wired.

func TestStatement_ExecContext_DelegatesToConnection(t *testing.T) {
	st := newTestStatement(t, "select 1")
	res, err := st.ExecContext(context.Background(), nil)
	assert.Nil(t, res)
	assert.Error(t, err)
}

func TestStatement_QueryContext_DelegatesToConnection(t *testing.T) {
	st := newTestStatement(t, "select 1")
	rows, err := st.QueryContext(context.Background(), nil)
	assert.Nil(t, rows)
	assert.Error(t, err)
}

// Compile-time assertions the Statement implements the *Context interfaces.
func TestStatement_ImplementsContextIfaces(t *testing.T) {
	var _ driver.StmtExecContext = (*Statement)(nil)
	var _ driver.StmtQueryContext = (*Statement)(nil)
}
