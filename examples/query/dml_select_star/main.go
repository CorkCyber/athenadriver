// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
	"log/slog"
	"os"
)

// main will query Athena and print all columns and rows information in csv format
func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		return
	}
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// 3. Query cancellation after 2 seconds
	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err := db.QueryContext(ctx, "select * from sampledb.elb_logs limit 3")
	if err != nil {
		println(err.Error())
		return
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample output:
request_timestamp,elb_name,request_ip,request_port,backend_ip,backend_port,request_processing_time,backend_processing_time,client_response_time,elb_response_code,backend_response_code,received_bytes,sent_bytes,request_verb,url,protocol,user_agent,ssl_cipher,ssl_protocol
2015-01-07T04:00:01.206255Z,elb_demo_005,245.85.197.169,8222,172.46.214.105,8888,0.001163,0.001233,0.000121,200,200,0,705,GET,http://www.example.com/images/858,HTTP/1.1,"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_11_6) AppleWebKit/602.1.50 (KHTML, like Gecko) Version/10.0 Safari/602.1.50",-,-
2015-01-07T04:00:01.612598Z,elb_demo_003,251.165.102.100,24615,172.41.185.247,80,0.000868,0.001232,0.000527,200,200,0,572,GET,https://www.example.com/images/905,HTTP/1.1,"Mozilla/5.0 (X11; CrOS x86_64 8172.45.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/51.0.2704.64 Safari/537.36",DHE-RSA-AES128-SHA,TLSv1.2
2015-01-07T04:00:02.793335Z,elb_demo_007,250.120.176.53,24251,172.55.212.88,80,0.00087,0.001561,0.001009,200,200,0,2040,GET,http://www.example.com/articles/518,HTTP/1.1,"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/42.0.2311.135 Safari/537.36 Edge/12.246",-,-
*/
