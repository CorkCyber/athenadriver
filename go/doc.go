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
// To capture driver logs, attach a *slog.Logger to the context you hand to
// db.QueryContext / db.ExecContext under the LoggerKey value:
//
//	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
//	    Level: slog.LevelInfo,
//	}))
//	ctx := context.WithValue(ctx, athenadriver.LoggerKey, logger)
//	rows, err := db.QueryContext(ctx, "SELECT 1")
//
// The logger is read once per pooled connection (in SQLConnector.Connect),
// so the first ctx that opens a given conn wins. Subsequent queries on the
// same pooled conn use that logger. Use separate *sql.DB instances if you
// need per-query logger swapping.
//
// The driver intentionally does not consult slog.Default(). Apps that set a
// global default handler will not see driver logs spill into it unless they
// explicitly opt in via LoggerKey above.
//
// To silence the driver after a logger has been wired in, call
// Config.LoggingEnabled = false. This short-circuits DriverTracer.Log before
// attrs are formatted and forces Logger() to return the discard logger,
// regardless of what was passed via LoggerKey. It is the hard kill-switch.
package athenadriver
