// SPDX-License-Identifier: MIT

package queryfx

import (
	"database/sql"

	"github.com/CorkCyber/athenadriver/athenareader/lib/configfx"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

// QueryAndDBConnection is the result of queryfx module
type QueryAndDBConnection struct {
	// DB is the pointer to sql/database DB
	DB *sql.DB
	// Query is the query string
	Query []string
}

// New opens the Athena connection described by the given config.
func New(mc configfx.AthenaDriverConfig) (QueryAndDBConnection, error) {
	db, err := sql.Open(drv.DriverName, mc.DrvConfig.Stringify())
	if err != nil {
		return QueryAndDBConnection{}, err
	}
	return QueryAndDBConnection{
		DB:    db,
		Query: mc.QueryString,
	}, nil
}
