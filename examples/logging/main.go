// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"log/slog"
	"os"

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
	conf.LoggingEnabled = true

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// 3. Query cancellation after 2 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, drv.LoggerKey, logger)
	rows, err := db.QueryContext(ctx, "select count(*) from sampledb.elb_logs")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()
}

/*
Sample Output:
{"time":"2026-06-15T13:44:26Z","level":"WARN","msg":"query canceled","queryID":"34e08219-ca2e-4e10-94b3-0ebf6c4c22f6"}
2026/06/15 13:44:26 context deadline exceeded
*/
