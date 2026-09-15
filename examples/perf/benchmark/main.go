// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand"
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

	// 2. Open Connection: one shared pool for the whole benchmark.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	defer db.Close()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: drv.DebugLevel}))

	// Pool monitoring: started once, stopped when the benchmark ends.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-ticker.C:
				logDBStats2(db.Stats(), logger)
			case <-done:
				return
			}
		}
	}()

	var wg sync.WaitGroup
	numGoRoutine := 10000
	wg.Add(2 * numGoRoutine)
	for i := range numGoRoutine {
		go func(i int, conf *drv.Config) {
			defer wg.Done()
			// 3. Query cancellation after 2 seconds
			ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
			// 3. Query
			query := "SELECT \"" + randString(drv.MAXQueryStringLength-32) + "\""
			r, e := db.QueryContext(ctx, query)
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
		}(i, conf)
		go func(i int, conf *drv.Config) {
			defer wg.Done()
			// 3. Query cancellation after 2 seconds
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
		}(i, conf)
	}
	wg.Wait()
}

// logDBStats is to log DB statistics.
func logDBStats2(stats sql.DBStats, logger *slog.Logger) {
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

func randString(l int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	s := make([]byte, l)
	for i := range l {
		s[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(s)
}
