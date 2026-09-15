// SPDX-License-Identifier: MIT

// This example uses the OpenTelemetry adapter shipped in
// github.com/CorkCyber/athenadriver/scope/otel. Alternatives:
// scope/tally (uber-go/tally) and scope/statsd (cactus/go-statsd-client).
package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	secret "github.com/CorkCyber/athenadriver/examples/constants"
	otelscope "github.com/CorkCyber/athenadriver/scope/otel"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. OpenTelemetry setup — stdout exporter for demo purposes.
	// Swap in an OTLP exporter (go.opentelemetry.io/otel/exporters/otlp/otlpmetric)
	// to ship to a real collector.
	exporter, err := stdoutmetric.New()
	if err != nil {
		log.Fatal(err)
	}
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(2*time.Second))),
	)
	defer provider.Shutdown(context.Background())
	otel.SetMeterProvider(provider)

	// 2. Driver config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		log.Fatal(err)
	}
	conf.MetricsEnabled = true

	// 3. Attach the otel Scope adapter via WithScope: applies
	// deterministically to every connection this connector produces,
	// unlike the ctx-value (MetricsKey) form, which pooled connections
	// pick up inconsistently.
	connector := drv.NewConnector(conf).WithScope(otelscope.New(otel.Meter("athenadriver_example")))
	db := sql.OpenDB(connector)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, "select count(*) from sampledb.elb_logs")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
}

/*
The stdout exporter prints periodic metric snapshots. In real deployments
swap it for an OTLP exporter targeting your collector / Prometheus /
Datadog / etc.
*/
