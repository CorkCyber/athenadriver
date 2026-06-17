// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"log"

	drv "github.com/CorkCyber/athenadriver/go"
)

var (
	ctx context.Context
	db  *sql.DB
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf := drv.NewNoOpsConfig()

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// A *DB is a pool of connections. Call Conn to reserve a connection for
	// exclusive use.
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.
		LevelSerializable})
	if err != nil {
		log.Fatal(err)
	}
	_, execErr := tx.Exec("SELECT request_timestamp,elb_name "+
		"from sampledb.elb_logs where url=? limit 1",
		"https://www.example.com/jobs/878")
	if execErr != nil {
		_ = tx.Rollback()
		log.Fatal(execErr)
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}
