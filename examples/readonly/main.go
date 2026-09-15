// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
	"log/slog"
	"os"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		log.Fatal(err)
		return
	}
	conf.ReadOnly = true

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// 3. Create Table with CTAS statement
	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err := db.QueryContext(ctx, "CREATE TABLE sampledb.elb_logs_new AS "+
		"SELECT * FROM sampledb.elb_logs limit 10;")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()
}

/*
Sample Output:
{"time":"2026-06-15T13:44:26Z","level":"WARN","msg":"write db violation","query":"CREATE TABLE sampledb.elb_logs_new AS SELECT * FROM sampledb.elb_logs limit 10;"}
2026/06/15 13:44:26 writing to Athena database is disallowed in read-only mode
*/
