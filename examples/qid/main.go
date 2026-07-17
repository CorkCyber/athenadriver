// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"os"

	"log/slog"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/go"
)

// main will query Athena and print all columns and rows information in csv format
func main() {
	// 1. Set AWS Credential in Driver Config.
	os.Setenv("AWS_SDK_LOAD_CONFIG", "1")
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		return
	}
	// 2. Open Connection.
	conf.MoneyWise = true
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// 3. Query
	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err := db.QueryContext(ctx, `3e6d49a6-999c-46ef-8295-7a101c327f90`)
	if err != nil {
		println(err.Error())
		return
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample output:
query cost: 0.0 USD, scanned data: 0 B, qid: 3e6d49a6-999c-46ef-8295-7a101c327f90
elb_name
elb_demo_006
*/
