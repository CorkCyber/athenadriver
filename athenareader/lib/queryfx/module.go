// SPDX-License-Identifier: MIT

package queryfx

import (
	"database/sql"

	"github.com/CorkCyber/athenadriver/athenareader/lib/configfx"
	drv "github.com/CorkCyber/athenadriver/go"
	"go.uber.org/fx"
)

// Module is to provide dependency of query to main app
var Module = fx.Provide(new)

// Params defines the dependencies or inputs
type Params struct {
	fx.In

	// MyConfig is the current Athenadriver Config
	MyConfig configfx.AthenaDriverConfig
}

// Result defines output
type Result struct {
	fx.Out

	// QAD is the Query and DB Connection
	QAD QueryAndDBConnection
}

// QueryAndDBConnection is the result of queryfx module
type QueryAndDBConnection struct {
	// DB is the pointer to sql/database DB
	DB *sql.DB
	// Query is the query string
	Query []string
}

func new(p Params) (Result, error) {
	// Open Connection.
	dsn := p.MyConfig.DrvConfig.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	qad := QueryAndDBConnection{
		DB:    db,
		Query: p.MyConfig.QueryString,
	}
	return Result{
		QAD: qad,
	}, nil
}
