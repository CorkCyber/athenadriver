// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region, secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		panic(err)
	}
	conf.LoggingEnabled = true

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)

	// 3. Query with pseudo command
	var s string
	_ = db.QueryRow("pc:stop_query_id c89088ab-595d-4ee6-a9ce-73b55aeb8953").Scan(&s)
	println("Stop Query ID c89088ab-595d-4ee6-a9ce-73b55aeb8953 returns:", s)
}

/*
Sample Output:
Stop Query ID c89088ab-595d-4ee6-a9ce-73b55aeb8953 returns: OK
*/
