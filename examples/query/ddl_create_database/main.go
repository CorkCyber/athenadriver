// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"

	drv "github.com/CorkCyber/athenadriver/go"
)

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

	var rows *sql.Rows
	rows, err = db.Query("CREATE DATABASE IF NOT EXISTS clickstreams" +
		"	COMMENT 'Site Foo clickstream data aggregates'" +
		"	LOCATION 's3://myS3location/clickstreams/'" +
		"	WITH DBPROPERTIES ('creator'='Jane D.', 'Dept.'='Marketing analytics');")
	if err != nil {
		log.Fatal(err)
		return
	}
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
*/
