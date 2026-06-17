// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"

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
		panic(err)
	}
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	/*query := "WITH dataset AS(SELECT '{\"name\": \"Susan Smith\"," +
	"\"org\": \"engineering\",\r\n" +
	"\"projects\": [{\"name\":\"project1\", \"completed\":false},\r\n" +
	"{\"name\":\"project2\", \"completed\":true}]}'\r\n" +
	"AS blob)\r\n" +
	"SELECT\r\njson_extract(blob, '$.name') AS name,\r\n" +
	"json_extract(blob, '$.projects') AS projects\r\n" +
	"FROM dataset"*/
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// 3. Query cancellation after 2 seconds
	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err := db.QueryContext(ctx, "SELECT JSON '\"Hello Athena\"'")
	if err != nil {
		println(err.Error())
		return
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample output:
name,projects
"Susan Smith",[{"name":"project1","completed":false},{"name":"project2","completed":true}]

Sample output:
_col0
2020-02-02T10:47:16.070-0800    DEBUG   go/observability.go:103 type: json      {"val": "\"Hello Athena\""}
"Hello Athena"
*/
