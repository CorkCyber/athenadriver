// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"

	drv "github.com/CorkCyber/athenadriver/go"
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
	rows, err := db.Query("desc sampledb.elb_logs")
	if err != nil {
		log.Fatal(err)
		return
	}
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
col_name,data_type,comment
request_timestamp,string,
elb_name,string,
request_ip,string,
request_port,int,
backend_ip,string,
backend_port,int,
request_processing_time,double,
backend_processing_time,double,
client_response_time,double,
elb_response_code,string,
backend_response_code,string,
received_bytes,bigint,
sent_bytes,bigint,
request_verb,string,
url,string,
protocol,string,
user_agent,string,
ssl_cipher,string,
ssl_protocol,string,
*/
