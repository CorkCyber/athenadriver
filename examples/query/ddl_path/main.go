// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	"log/slog"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/go"
)

// Note:
// Amazon Redshift Spectrum support $path and $size
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_EXTERNAL_TABLE.html#r_CREATE_EXTERNAL_TABLE_usage-pseudocolumns
// But Athena supports only $path
func main() {
	// 1. Set AWS Credential in Driver Config.
	os.Setenv("AWS_SDK_LOAD_CONFIG", "1")
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
	ctx := context.WithValue(context.Background(), drv.LoggerKey, logger)
	rows, err = db.QueryContext(ctx, "SELECT \"$path\" as s3_filepath from sampledb.elb_logs limit 1")
	if err != nil {
		log.Fatal(err)
		return
	}
	println(drv.ColsRowsToCSV(rows))
	// skip $size in TidySQL to ensure the raw string is passed in
	rows, err = db.QueryContext(ctx, "SELECT  \"$path\", \"$size\" from sampledb.elb_logs limit 1")
	if err != nil {
		log.Fatal(err)
		return
	}
	println(drv.ColsRowsToCSV(rows))
}

/*
Sample Output:
s3_filepath
s3://athena-examples-us-east-1/elb/plaintext/2015/01/03/part-r-00014-ce65fca5-d6c6-40e6-b1f9-190cc4f93814.txt

{"level":"error","ts":1592019844.947993,"caller":"go/trace.go:129","msg":"QueryExecutionStateFailed","workgroup":"primary","queryID":"49c8ad1c-13c6-48e5-ba65-061934b3dde4","reason":"SYNTAX_ERROR: line 1:17: Column '$size' cannot be resolved","stacktrace":"github.com/CorkCyber/athenadriver/go.(*DriverTracer).Log\n\t/opt/share/go/path/src/github.com/CorkCyber/athenadriver/go/trace.go:129\ngithub.com/CorkCyber/athenadriver/go.(*Connection).QueryContext\n\t/opt/share/go/path/src/github.com/CorkCyber/athenadriver/go/connection.go:393\ndatabase/sql.ctxDriverQuery\n\t/opt/share/yuanma/go_src/src/database/sql/ctxutil.go:48\ndatabase/sql.(*DB).queryDC.func1\n\t/opt/share/yuanma/go_src/src/database/sql/sql.go:1592\ndatabase/sql.withLock\n\t/opt/share/yuanma/go_src/src/database/sql/sql.go:3184\ndatabase/sql.(*DB).queryDC\n\t/opt/share/yuanma/go_src/src/database/sql/sql.go:1587\ndatabase/sql.(*DB).query\n\t/opt/share/yuanma/go_src/src/database/sql/sql.go:1570\ndatabase/sql.(*DB).QueryContext\n\t/opt/share/yuanma/go_src/src/database/sql/sql.go:1547\nmain.main\n\t/opt/share/go/path/src/github.com/CorkCyber/athenadriver/examples/query/ddl_path.go:51\nruntime.main\n\t/opt/share/yuanma/go_src/src/runtime/proc.go:203"}
2020/06/12 20:44:04 SYNTAX_ERROR: line 1:17: Column '$size' cannot be resolved
*/
