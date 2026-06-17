// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"
	"strconv"

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
	rows, err := db.Query("select nan(), infinity(), url, " +
		"request_port from sampledb.elb_logs limit 3")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()

	var real, inf float64
	var url string
	var request_port int
	for rows.Next() {
		if err := rows.Scan(&real, &inf, &url,
			&request_port); err != nil {
			log.Fatal(err)
		}
		println(strconv.FormatFloat(real, 'f', 6, 64) + "," +
			strconv.FormatFloat(inf, 'f', 6, 64) + "," + url + "," +
			strconv.Itoa(request_port))
	}
}

/*
Sample Output:
NaN,+Inf,http://www.example.com/images/386,8096
NaN,+Inf,https://www.example.com/jobs/132,24938
NaN,+Inf,https://www.example.com/images/229,7963
*/
