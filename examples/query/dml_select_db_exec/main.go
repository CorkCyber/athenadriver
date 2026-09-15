// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		println(err.Error())
		return
	}
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Query and print results
	result, err := db.Exec("SELECT url from sampledb.elb_logs limit 10")
	if err != nil {
		println(err.Error())
		return
	}
	i, _ := result.RowsAffected()
	println(i)
	i, _ = result.LastInsertId()
	println(i)
}

/*
Sample Output:
0
-1
*/
