// SPDX-License-Identifier: MIT

// Package athenadriver is a fully-featured Go database/sql driver for
// Amazon AWS Athena. Originally developed at Uber Technologies Inc.,
// forked by Grafana Labs for aws-sdk-go-v2, and maintained by Cork
// Cyber on this branch.
//
// It provides a hassle-free way of querying AWS Athena database with Go
// standard library. It not only provides basic features of Athena Go SDK, but
// addresses some of its limitation, improves and extends it. Except the basic
// features provided by Go database/sql like error handling, database pool
// and reconnection, athenadriver supports the following features out of box:
//
//   - Support multiple AWS authorization methods
//   - Full support of Athena Basic Data Types
//   - Full support of Athena Advanced Type for queries with Geospatial identifiers, ML and UDFs
//   - Full support of ALL Athena Query Statements, including DDL, DML and UTILITY
//   - Support newly added INSERT INTO...VALUES
//   - Athena workgroup and tagging support including remote workgroup creation
//   - Go sql's Prepared statement support
//   - Go sql's DB.Exec() and db.ExecContext() support
//   - Query cancelling support
//   - Mask columns with specific values
//   - Database missing value handling
//   - Read-Only mode
//   - Bring-your-own metrics and OpenTelemetry-compatible tracing spans
//
// Amazon Athena is an interactive query service that lets you use standard
// SQL to analyze data directly in Amazon S3. You can point Athena at your data
// in Amazon S3 and run ad-hoc queries and get results in seconds. Athena is
// serverless, so there is no infrastructure to set up or manage. You pay only
// for the queries you run. Athena scales automatically—executing queries
// in parallel—so results are fast, even with large datasets and complex queries.
//
// # Logging
//
// The driver uses log/slog. By default it is silent: every log record is
// routed to an internal discard handler, so importing this package does not
// emit anything until you opt in.
//
// To capture driver logs, prefer SQLConnector.WithLogger, which applies
// deterministically to every connection a connector produces:
//
//	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
//	    Level: slog.LevelInfo,
//	}))
//	connector := athenadriver.NewConnector(conf).WithLogger(logger)
//	db := sql.OpenDB(connector)
//
// A LoggerKey ctx-value form also exists but is NOT deterministic under a
// pool: database/sql's background connectionOpener calls Connect with a
// valueless context.Background(), so whether a pooled connection sees your
// logger depends on which path built it. Use WithLogger instead.
//
// The driver never consults slog.Default(): a global handler won't see
// driver logs unless you opt in via WithLogger/LoggerKey.
//
// To silence the driver, set Config.LoggingEnabled = false before passing
// Config to NewConnector (it's copied, so mutating the original afterwards
// does nothing), or DSN key LoggingEnabled=false. Hard kill-switch, not
// runtime-mutable; use DriverTracer.SetLogger for that.
//
// # Metrics
//
// Counters/timers via the 2-method Scope interface, enabled by default but
// no-op until you supply one, same WithScope/MetricsKey pattern as
// Logging. Adapters: scope/otel, scope/tally, scope/statsd.
//
// # Tracing
//
// One span per QueryContext/ExecContext call via Tracer/Span, enabled by
// default but no-op until you supply one via WithTracer (or TracerKey for
// a per-query override). Tagged per OTel's database semantic conventions
// (db.system.name=aws.athena, db.namespace, db.operation.name,
// server.address) plus athena.* attributes, so any backend that understands
// those conventions renders it as a DB call. scope/otel.NewTracer bridges
// to OpenTelemetry.
package athenadriver
