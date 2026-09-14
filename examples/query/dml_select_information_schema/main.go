// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

// main will query Athena and print all columns and rows information in csv format
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
	rows, err := db.Query("SELECT * FROM information_schema." +
		"columns where table_schema='sampledb' and table_name='elb_logs';")
	if err != nil {
		println(err.Error())
		return
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample output:
table_catalog,table_schema,table_name,column_name,ordinal_position,column_default,is_nullable,data_type,comment,extra_info
awsdatacatalog,sampledb,elb_logs,request_timestamp,1,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,elb_name,2,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,request_ip,3,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,request_port,4,,YES,integer,,
awsdatacatalog,sampledb,elb_logs,backend_ip,5,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,backend_port,6,,YES,integer,,
awsdatacatalog,sampledb,elb_logs,request_processing_time,7,,YES,double,,
awsdatacatalog,sampledb,elb_logs,backend_processing_time,8,,YES,double,,
awsdatacatalog,sampledb,elb_logs,client_response_time,9,,YES,double,,
awsdatacatalog,sampledb,elb_logs,elb_response_code,10,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,backend_response_code,11,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,received_bytes,12,,YES,bigint,,
awsdatacatalog,sampledb,elb_logs,sent_bytes,13,,YES,bigint,,
awsdatacatalog,sampledb,elb_logs,request_verb,14,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,url,15,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,protocol,16,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,user_agent,17,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,ssl_cipher,18,,YES,varchar,,
awsdatacatalog,sampledb,elb_logs,ssl_protocol,19,,YES,varchar,,
*/
