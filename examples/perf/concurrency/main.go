// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(
		"s3://qr-athena-query-result/",
		secret.Region,
		secret.AccessID,
		secret.SecretAccessKey)
	if err != nil {
		panic(err)
	}

	// 2. Open Connection: one shared pool for all goroutines.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	defer db.Close()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: drv.DebugLevel}))

	// Pool monitoring: started once, stopped when the run ends.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-ticker.C:
				logDBStats(db.Stats(), logger)
			case <-done:
				return
			}
		}
	}()

	var wg sync.WaitGroup
	numGoRoutine := 100
	wg.Add(numGoRoutine)
	for i := range numGoRoutine {
		go func(i int, conf *drv.Config) {
			defer wg.Done()
			ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
			// 3. Query
			r, e := db.QueryContext(ctx, "SHOW FUNCTIONS")
			if e != nil {
				fmt.Fprintf(os.Stderr, "[%v]%s\n", i, e.Error())
				return
			} else {
				print(i, ",")
			}
			defer r.Close()
			cnt := 0
			for r.Next() {
				cnt++
			}
			if err := r.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "[%v]%s\n", i, err.Error())
			}
		}(i, conf)
	}
	wg.Wait()
}

// logDBStats is to log DB statistics.
func logDBStats(stats sql.DBStats, logger *slog.Logger) {
	logger.Info("DBPoolStatus",
		slog.Int("MaxOpenConnections", stats.MaxOpenConnections),
		slog.Int("OpenConnections", stats.OpenConnections),
		slog.Int("Idle", stats.Idle),
		slog.Int("InUse", stats.InUse),
		slog.Int64("WaitCount", stats.WaitCount),
		slog.Duration("WaitDuration", stats.WaitDuration),
		slog.Int64("MaxIdleClosed", stats.MaxIdleClosed),
		slog.Int64("MaxLifetimeClosed", stats.MaxLifetimeClosed),
	)
}
