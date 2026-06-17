// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"fmt"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		return
	}
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Query and print results
	var url string
	_ = db.QueryRow("SELECT url from sampledb.elb_logs limit 1").Scan(&url)
	fmt.Println(url)
}

/*
Sample Output:
https://www.example.com/jobs/553
*/
