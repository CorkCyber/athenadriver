// SPDX-License-Identifier: MIT

package main

/*
// The type of query statement that was run. DDL indicates DDL query statements.
// DML indicates DML (Data Manipulation Language) query statements, such as
// CREATE TABLE AS SELECT. UTILITY indicates query statements other than DDL
// and DML, such as SHOW CREATE TABLE, or DESCRIBE <table>.
StatementType *string `type:"string" enum:"StatementType"`


*/

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"

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
	rows, err := db.Query("ALTER TABLE testme2 DROP PARTITION (ssl_protocol = 'TLSv1.2')")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
*/
