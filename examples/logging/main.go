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
	drv "github.com/CorkCyber/athenadriver/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	conf.LoggingEnabled = true
	if err != nil {
		log.Fatal(err)
		return
	}

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
{"level":"warn","ts":1579990455.5467792,"caller":"go/observability.go:73","msg":"query canceled","resp.QueryExecutionId":"34e08219-ca2e-4e10-94b3-0ebf6c4c22f6"}
2020/01/25 14:14:15 context deadline exceeded
*/
