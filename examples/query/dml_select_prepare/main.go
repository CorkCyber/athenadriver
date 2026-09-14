// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	var conf *drv.Config
	var err error
	if conf, err = drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey); err != nil {
		panic(err)
	}
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Query and print results
	if _, err = db.Exec("DROP TABLE IF EXISTS sampledb.urls"); err != nil {
		panic(err)
	}

	statement, err := db.Prepare("CREATE TABLE sampledb.urls AS " +
		"SELECT url FROM sampledb.elb_logs where request_ip=? limit ?")
	if err != nil {
		panic(err)
	}
	if result, e := statement.Exec("244.157.42.179", 2); e == nil {
		if rowsAffected, err := result.RowsAffected(); err == nil {
			println(rowsAffected)
		}
	}

	rows, err := db.Query("SELECT request_timestamp,elb_name "+
		"from sampledb.elb_logs where url=? limit 1",
		"https://www.example.com/jobs/878")
	if err != nil {
		return
	}
	println(drv.ColsRowsToCSV(rows))

}

/*
Sample Output:
2
request_timestamp,elb_name
2015-01-02T04:11:59.697912Z,elb_demo_007
*/
