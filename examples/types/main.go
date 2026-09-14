// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

// main will query Athena and print all columns and rows information in csv format
func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		panic(err)
	}
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	query := "select JSON '\"Hello Athena\"', " +
		"ST_POINT(-74.006801, 40.70522), " +
		"ROW(1, 2.0),  INTERVAL '2' DAY, " +
		"INTERVAL '3' MONTH, " +
		"TIME '01:02:03.456', " +
		"TIME '01:02:03.456 America/Los_Angeles', " +
		"TIMESTAMP '2001-08-22 03:04:05.321 America/Los_Angeles';"
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: drv.DebugLevel}))
	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
select TIMESTAMP '2001-08-22 03:04:05.321 PDT';
SYNTAX_ERROR: line 1:145: '2001-08-22 03:04:05.321 PDT' is not a valid timestamp literal

Sample output:
_col0,_col1,_col2,_col3,_col4,_col5
2 00:00:00.000,0-3,0000-01-01T01:02:03.456-07:52,0000-01-01T01:02:03.456-07:52,2001-08-22T03:04:05.321-07:00,2001-08-22T03:04:05.321-07:00
*/
