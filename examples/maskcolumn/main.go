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
	conf.SetMaskedColumnValue("url", "xxx")
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	rows, err := db.Query("select request_timestamp, url from sampledb.elb_logs limit 3")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()

	var requestTimestamp string
	var url string
	for rows.Next() {
		if err := rows.Scan(&requestTimestamp, &url); err != nil {
			log.Fatal(err)
		}
		println(requestTimestamp + "," + url)
	}
}

/*
Sample Output:
2015-01-03T12:00:00.516940Z,xxx
2015-01-03T12:00:00.902953Z,xxx
2015-01-03T12:00:01.206255Z,xxx
*/
