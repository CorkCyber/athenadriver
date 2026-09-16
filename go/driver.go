// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
)

// SQLDriver is an implementation of sql/driver interface for AWS Athena.
// https://vyskocilm.github.io/blog/implement-sql-database-driver-in-100-lines-of-go/
// https://golang.org/pkg/database/sql/driver/#Driver
type SQLDriver struct{}

func init() {
	sql.Register(DriverName, &SQLDriver{})
}

// Open returns a new connection to AWS Athena.
// The dsn is a string in a driver-specific format.
// the sql package maintains a pool of idle connections for efficient re-use.
// The returned connection is only used by one goroutine at a time.
func (d *SQLDriver) Open(dsn string) (driver.Conn, error) {
	config, err := NewConfig(dsn)
	if err != nil {
		return nil, err
	}
	c := &SQLConnector{
		config: config,
	}
	return c.Connect(context.Background())
}

// Validate is a startup-time DSN sanity check: it returns the same parse
// error Open would on a malformed DSN, without opening a connection.
// database/sql does not have a driver-level Validator interface, so this is
// a plain helper callable from app code. The Conn-level driver.Validator
// is implemented on *Connection.
func (d *SQLDriver) Validate(dsn string) error {
	_, err := NewConfig(dsn)
	return err
}

// OpenConnector will be called upon query execution.
// If a Driver implements DriverContext.OpenConnector, then sql.DB will call
// OpenConnector to obtain a Connector and then invoke
// that Connector's Conn method to obtain each needed connection,
// instead of invoking the Driver's Open method for each connection.
// The two-step sequence allows drivers to parse the name just once
// and also provides access to per-Conn contexts.
func (d *SQLDriver) OpenConnector(dsn string) (driver.Connector, error) {
	config, err := NewConfig(dsn)
	if err != nil {
		return nil, err
	}
	return &SQLConnector{config: config}, nil
}
