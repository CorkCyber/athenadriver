// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/go"
)

// main will query Athena and print all columns and rows information in csv format
func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		return
	}
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	query := "SELECT a.elb_name, " +
		"a.url FROM elb_logs_new2 a LEFT JOIN sampledb.elb_logs_new b ON a." +
		"request_timestamp = b.request_timestamp"
	rows, err := db.Query(query)
	if err != nil {
		return
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample output:
elb_name,url
elb_demo_009,https://www.example.com/articles/746
*/
