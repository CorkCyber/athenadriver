// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatement_NumInput(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	dn := "123"
	d := []driver.Value{
		dn,
	}
	r, e := st.Exec(d)
	assert.NotNil(t, e)
	assert.Nil(t, r)
	assert.Equal(t, st.NumInput(), 1)
}

func TestStatement_Exec(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	dn := "123"
	d := []driver.Value{
		dn,
	}
	_, e := st.Exec(d)
	assert.NotNil(t, e)
}

func TestStatement_Exec_After_Close(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	dn := "123"
	d := []driver.Value{
		dn,
	}
	st.Close()
	_, err := st.Exec(d)
	assert.Equal(t, driver.ErrBadConn, err)
}

func TestStatement_Query(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	dn := "123"
	d := []driver.Value{
		dn,
	}
	_, e := st.Query(d)
	assert.NotNil(t, e)
}

func TestStatement_Query_After_Close(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	dn := "123"
	d := []driver.Value{
		dn,
	}
	st.Close()
	_, err := st.Query(d)
	assert.Equal(t, driver.ErrBadConn, err)
}

func TestStatement_ColumnConverter(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	assert.NotNil(t, st.ColumnConverter(0))
}

func TestStatement_Close(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	assert.Equal(t, st.NumInput(), 1)
	st.Close()
	assert.Equal(t, 0, st.NumInput())
}

func TestStatement_Close_AfterConnectionClose(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, _ := connector.Connect(context.Background())
	st := Statement{
		connection: conn.(*Connection),
		query:      "abc=?",
	}
	conn.Close()
	st.connection = nil
	assert.Equal(t, st.NumInput(), 1)
	// Close is idempotent per driver.Stmt contract (golang/go#16019);
	// returning ErrBadConn causes database/sql to evict the parent conn.
	assert.NoError(t, st.Close())
	assert.NoError(t, st.Close())
}
