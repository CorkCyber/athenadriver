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

	wgTags := drv.NewWGTags()
	wgTags.AddTag("Uber User", "henry.wu")
	wgTags.AddTag("Uber ID", "123456")
	wgTags.AddTag("Uber Role", "SDE")
	// Specify workgroup name henry_wu should be used for the following query
	wg := drv.NewWG("henry_wu", nil, wgTags)
	_ = conf.SetWorkGroup(wg)
	// comment out the line below to allow remote workgroup creation and the query will be successful!!!
	//conf.SetWGRemoteCreationAllowed(false)

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	rows, err := db.Query("select request_timestamp, url from sampledb.elb_logs limit 3")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()

	var requestTimestamp string
	var url string
	for rows.Next() {
		if err := rows.Scan(&requestTimestamp, &url); err != nil {
			log.Fatal(err)
		}
		println(requestTimestamp + "," + url)
	}
}

/*
Sample Output:
2020/01/20 15:29:52 Workgroup henry_wu doesn't exist and workgroup remote creation is disabled.

After commenting out `conf.SetWGRemoteCreationAllowed(false)` at line 27:
2015-01-07T16:00:00.516940Z,https://www.example.com/articles/553
2015-01-07T16:00:00.902953Z,http://www.example.com/images/501
2015-01-07T16:00:01.206255Z,https://www.example.com/images/183

and you will see a new workgroup named `henry_wu` is created in AWS Athena console: https://us-east-2.console.aws.amazon.com/athena/workgroups/home?region=us-east-2
*/
