// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	"log/slog"
	"os"

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
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var rows *sql.Rows
	rows, err = db.Query("DROP VIEW IF EXISTS sampledb.elb_logs_view;")
	if err != nil {
		log.Fatal(err)
		return
	}
	rows, err = db.Query("CREATE VIEW sampledb.elb_logs_view AS SELECT * FROM sampledb.elb_logs limit 1;")
	if err != nil {
		log.Println(err)
	}

	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err = db.QueryContext(ctx, "describe sampledb.elb_logs_view")
	if err != nil {
		log.Fatal(err)
		return
	}
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
column,type
request_timestamp,varchar
elb_name,varchar
request_ip,varchar
request_port,integer
backend_ip,varchar
backend_port,integer
request_processing_time,double
backend_processing_time,double
client_response_time,double
elb_response_code,varchar
backend_response_code,varchar
received_bytes,bigint
sent_bytes,bigint
request_verb,varchar
url,varchar
protocol,varchar
user_agent,varchar
ssl_cipher,varchar
ssl_protocol,varchar

*/
