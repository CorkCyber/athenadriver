// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
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
	rows, err := db.Query("SELECT ARRAY [1,2,3,4] AS items")
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
items
[1, 2, 3, 4]
*/
