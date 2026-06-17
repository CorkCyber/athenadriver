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
	drv "github.com/CorkCyber/athenadriver/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	os.Setenv("AWS_SDK_LOAD_CONFIG", "1")
	conf, err := drv.NewDefaultConfig(
		"s3://qr-athena-query-result/",
		secret.Region,
		secret.AccessID,
		secret.SecretAccessKey)
	os.Setenv("AWS_REGION", "us-east-1")
	if err != nil {
		panic(err)
	}
	var wg sync.WaitGroup
	numGoRoutine := 10000
	wg.Add(numGoRoutine)
	for i := range numGoRoutine {
		// 2. Open Connection.
		db, _ := sql.Open(drv.DriverName, conf.Stringify())
		logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: drv.DebugLevel}))
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

		go func(db *sql.DB, logger *slog.Logger) {
			for range time.Tick(2 * time.Second) {
				stats := db.Stats()
				logDBStats2(stats, logger)
			}
		}(db, logger)
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
