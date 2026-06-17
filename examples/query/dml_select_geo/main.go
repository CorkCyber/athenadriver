// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/go"
	"log/slog"
	"os"
)

// main will query Athena and print all columns and rows information in csv format
func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		log.Fatal(err)
		return
	}
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err := db.QueryContext(ctx, "SELECT ST_POINT(-74.006801, 40.705220);")
	if err != nil {
		println(err.Error())
		return
	}
	defer rows.Close()
	drv.ColsToCSV(rows)
	// array, map, binary, structure are returned as string type.
	var items string
	for rows.Next() {
		if err := rows.Scan(&items); err != nil {
			log.Fatal(err)
		}
	}
	println(items)
}

/*
Sample output:
_col0
00 00 00 00 01 01 00 00 00 20 25 76 6d 6f 80 52 c0 18 3e 22 a6 44 5a 44 40

After adding logging:
_col0
2020-02-02T10:51:37.355-0800    DEBUG   go/observability.go:103 type: varbinary {"val": "00 00 00 00 01 01 00 00 00 20 25 76 6d 6f 80 52 c0 18 3e 22 a6 44 5a 44 40"}
00 00 00 00 01 01 00 00 00 20 25 76 6d 6f 80 52 c0 18 3e 22 a6 44 5a 44 40
*/
