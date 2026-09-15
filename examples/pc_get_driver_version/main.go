// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region, secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		panic(err)
	}
	conf.LoggingEnabled = true

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)

	// 3. Query with pseudo command `pc:get_driver_version`
	rows, err := db.QueryContext(context.Background(), "pc:get_driver_version")
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
_col0
1.1.6
*/
