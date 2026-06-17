// SPDX-License-Identifier: MIT

package main

import (
	"database/sql"
	"log"

	secret "github.com/CorkCyber/athenadriver/examples/constants"

	drv "github.com/CorkCyber/athenadriver/go"
)

// https://aws.amazon.com/about-aws/whats-new/2019/09/amazon-athena-adds-support-inserting-data-into-table-results-of-select-query/
//
// With this release,
// you can insert new rows into a destination table based on a SELECT query
// statement that runs on a source table,
// or based on a set of values that are provided as part of the query
// statement. Supported data formats include Avro, JSON, ORC, Parquet,
// and Text files.
//
// INSERT INTO statements can also help you simplify your ETL process.
// For example, you can use INSERT INTO to select data from a source table
// that is in JSON format and write to a destination table in Parquet format
// in a single query. INSERT INTO statements are charged based on bytes
// scanned in the Select phase,
// similar to how Athena charges for Select queries.
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
	rows, err := db.Query("INSERT INTO testme2 SELECT * FROM testme")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
rows
1
*/
