// SPDX-License-Identifier: MIT

package main

import (
	"context"
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
	// 3. Execute and print results
	if _, err = db.ExecContext(context.Background(),
		"DROP TABLE IF EXISTS sampledb.urls"); err != nil {
		panic(err)
	}

	var result sql.Result
	if result, err = db.Exec("CREATE TABLE sampledb.urls AS "+
		"SELECT url FROM sampledb.elb_logs where request_ip=? limit ?",
		"244.157.42.179", 1); err != nil {
		panic(err)
	}
	if rowsAffected, err := result.RowsAffected(); err == nil {
		println(rowsAffected)
	}

	if result, err = db.Exec("INSERT INTO sampledb.urls VALUES (?),(?),(?)",
		"abc", "efg", "xyz"); err != nil {
		panic(err)
	}
	if rowsAffected, err := result.RowsAffected(); err == nil {
		println(rowsAffected)
	}
}

/*
Sample Output:
1
3
*/
