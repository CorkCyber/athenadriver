// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"

	drv "github.com/CorkCyber/athenadriver/v2/go"
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

	// DDL returns no rows to read, so Exec is the right call here.
	if _, err = db.Exec("DROP TABLE IF EXISTS sampledb.elb_logs_new;"); err != nil {
		log.Fatal(err)
		return
	}
	rows, err := db.Query("CREATE TABLE sampledb.elb_logs_new AS	" +
		"SELECT * FROM sampledb.elb_logs limit 10;")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()

	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
rows
10
*/
