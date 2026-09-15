// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
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
		panic(err)
	}
	conf.LoggingEnabled = true

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// 3. Query cancellation after 2 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 2000*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, drv.LoggerKey, logger)
	e := db.PingContext(ctx)
	if e != nil {
		panic(e)
	}
	println("OK")
}

/*
Sample output:
(When setup is well done and connection is good)
OK

(otherwise)
panic: driver: bad connection

goroutine 1 [running]:
main.main()
        /opt/share/go/path/src/github.com/CorkCyber/athenadriver/examples/ping.go:35 +0x320
*/
