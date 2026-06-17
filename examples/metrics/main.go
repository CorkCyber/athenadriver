// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"io"
	"log"
	"time"

	"github.com/cactus/go-statsd-client/v5/statsd"
	tallystatsd "github.com/uber-go/tally/v4/statsd"

	"github.com/uber-go/tally/v4"
	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/go"
)

func newScope() (tally.Scope, io.Closer) {
	statter, _ := statsd.NewBufferedClient("127.0.0.1:8125",
		"stats", 100*time.Millisecond, 1440)

	reporter := tallystatsd.NewReporter(statter, tallystatsd.Options{
		SampleRate: 1.0,
	})

	scope, closer := tally.NewRootScope(tally.ScopeOptions{
		Prefix:   "henrywu_test_metrics_service",
		Tags:     map[string]string{},
		Reporter: reporter,
	}, time.Second)

	return scope, closer
}

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

	// 3. Query cancellation after 2 seconds
	// Create tally scope
	scope, _ := newScope()
	// Create context and attach tally scope with context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, drv.MetricsKey, scope)
	rows, err := db.QueryContext(ctx, "select count(*) from sampledb.elb_logs")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()
}

/*
Sample Output:
Run nc in another terminal, so you can use the metrics is reported like below:
$nc 8125 -l -u
stats.henrywu_test_metrics_service.awsathena.connector.connect:0.140147|ms
stats.henrywu_test_metrics_service.awsathena.query.workgroup:0.000607|msstats.henrywu_test_metrics_service.awsathena.query.startqueryexecution:1191.644566|msstats.henrywu_test_metrics_service.awsathena.query.queryexecutionstatesucceeded:3320.820154|ms
*/
