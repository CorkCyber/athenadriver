// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/go"
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
	rows, err := db.Query("select map(array['alice'], array['has a cat']);")
	if err != nil {
		println(err.Error())
		return
	}
	defer rows.Close()
	drv.ColsToCSV(rows)
	// array, map, binary, structure are returned as string type.
	var aMap string
	for rows.Next() {
		if err := rows.Scan(&aMap); err != nil {
			log.Fatal(err)
		}
	}
	println(aMap)
}

/*
Sample output:
_col0
{alice=has a cat}
*/
