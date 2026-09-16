
![](resources/logo.png)

[![GoDoc][doc-img]][doc]
[![Github release][release-img]][release]
[![Go Report Card][report-card-img]][report-card]
[![lic][license-img]][license]
[![made][made-img]][made]

----

:package: [athenadriver](https://github.com/CorkCyber/athenadriver/tree/main/go) - A fully-featured AWS Athena database driver for Go  
:shell: [athenareader](https://github.com/CorkCyber/athenadriver/tree/main/athenareader) - A moneywise command line utililty to query athena in command line.

----

## About this fork

This repository is a Cork Cyber fork of
[`grafana/athenadriver`](https://github.com/grafana/athenadriver), which
is itself a fork of
[`uber/athenadriver`](https://github.com/uber/athenadriver) (dormant
since 2025-05-31). The fork chain looks like:

```
uber/athenadriver  →  grafana/athenadriver  →  CorkCyber/athenadriver
   (dormant 2025)      (aws-sdk-go-v2 port)     (this repo)
```

The Grafana fork carried the v1 → v2 SDK port forward. This Cork Cyber
fork picks up from there and adds the surface area uber/grafana left on
the table:

- Athena API features added since 2022 — `ClientRequestToken`,
  `ResultReuseConfiguration`, `Catalog`, `EncryptionConfiguration`,
  `ExpectedBucketOwner`, `SubstatementType`, `AssumeRoleWithWebIdentity`,
  parameterized queries verified against v2.
- Standard-library logging via `log/slog` (drops the `zap` /
  `multierr` direct deps) and a path toward OpenTelemetry metrics +
  traces (see TODO).
- Connector-level `aws.Config` injection so callers can wire IMDS,
  IRSA / EKS pod identity, SSO, OIDC, assume-role chains, and custom
  retryers without touching the DSN.
- `database/sql` interface coverage: `RowsColumnType{ScanType,
  Nullable, PrecisionScale}`, `Statement.{ExecContext,QueryContext}`,
  `Connection.{IsValid,ResetSession}`, `Driver.Validate`.
- Hardening: paginator-based result streaming, region-aware cost
  estimator, exponential backoff between status polls, ctx-cancelled
  queries now actually issue `StopQueryExecution`.

The public API surface from the upstream is preserved where possible.
The notable break is the import path
(`github.com/CorkCyber/athenadriver/v2`, per Go semantic import
versioning); see CHANGELOG for the full migration notes.

Upstream contributions are welcome here; PRs that originally targeted
`uber/athenadriver` or `grafana/athenadriver` and never landed are good
candidates to re-open against this repo.

## Overview

`athenadriver` is a fully-featured AWS Athena database driver for Go,
originally developed at Uber Technologies Inc., carried forward by
Grafana Labs through the aws-sdk-go-v2 migration, and now maintained
here at Cork Cyber.
It provides a hassle-free way of querying AWS Athena database with Go standard
library. It not only provides basic features of Athena Go SDK, but 
addresses some SDK's limitation, improves and extends it. Moreover, it also includes
advanced features like Athena workgroup and tagging creation, driver read-only mode and so on.

The PDF version of AthenaDriver document is available at [ :scroll: ](resources/athenadriver.pdf)

## v2.0.0 Migration Guide

Ten breaking changes, batched into one release.

### Summary

| # | Change | Migration |
|---|--------|-----------|
| 1 | Module path `uber/athenadriver` → `CorkCyber/athenadriver/v2` | Rewrite imports (`.../v2/go`). `sql.Open("awsathena", dsn)` unchanged. |
| 2 | `aws-sdk-go` → `aws-sdk-go-v2` | Update AWS type imports + pointer helpers. |
| 3 | `Config` is a typed struct | Assign fields; a few validating setters remain. |
| 4 | Logger: `*zap.Logger` → `*slog.Logger` | Inject `*slog.Logger` via `LoggerKey` ctx. |
| 5 | Go floor: 1.13 → 1.26 | Bump toolchain. |
| 6 | `athenareader/` is a separate module | Update its import path if you used it as a library. |
| 7 | `ServiceLimitOverride` is a typed struct | Assign fields. |
| 8 | Observability + WG helper constructors collapsed | One constructor / struct literal. |
| 9 | Poll defaults changed (exponential backoff, 30s cap) | Opt out with two field assignments. |
| 10 | Metrics decoupled from tally | Import a `scope/*` adapter or write ~15 lines of glue. |

### 1. Module path

```
github.com/uber/athenadriver       (v1)
github.com/grafana/athenadriver    (v2 port, transitional)
github.com/CorkCyber/athenadriver/v2  (v2.0.0)
```

### 2. AWS SDK v1 → v2

- AWS types come from `github.com/aws/aws-sdk-go-v2/service/athena/types`.
- Credentials flow through `aws.Config` / `aws.CredentialsProvider`.
- DSN keys (`accessID`, `secretAccessKey`, `sessionToken`, `region`) unchanged.
- Escape hatch for IMDS / IRSA / SSO / OIDC / assume-role / custom retryers: `SQLConnector.WithAWSConfig(aws.Config)`.

### 3. `Config` is a typed struct

Field access replaces setter/getter methods. DSN format unchanged; `Stringify()` and `NewConfig(dsn)` round-trip every field.

```go
conf := drv.NewNoOpsConfig()
_ = conf.SetOutputBucket("s3://...")  // validating setter, kept
_ = conf.SetRegion("us-east-1")       // validating setter, kept
conf.DB        = "sampledb"
conf.MoneyWise = true
conf.Catalog   = "myCatalog"
```

Validating setters kept: `SetOutputBucket`, `SetRegion`, `SetAccessID`, `SetSecretAccessKey`, `SetWorkGroup`.

| pre-v2 | v2.0.0 |
|--------|--------|
| `cfg.SetDB("foo")` / `cfg.GetDB()` | `cfg.DB = "foo"` / `cfg.DB` |
| `cfg.SetMoneyWise(true)` / `cfg.IsMoneyWise()` | `cfg.MoneyWise = true` / `cfg.MoneyWise` |
| `cfg.GetRegion()` (env fallback) | `cfg.RegionOrEnv()` |
| `cfg.GetCatalog()` (default) | `cfg.CatalogOrDefault()` |
| `cfg.GetResultPollIntervalSeconds()` | `cfg.PollInterval()` |
| `cfg.SetResultPollIntervalSeconds(n)` | `cfg.ResultPollInterval = time.Duration(n) * time.Second` |
| `cfg.SetServiceLimitOverride(svc)` | `cfg.ServiceLimit = &svc` |
| `cfg.GetWorkgroup()` | `*cfg.WorkGroup` |

Same pattern applies to every other `Set*/Get*/Is*` pair on `Config`.

### 4. Logger: zap → log/slog

**Silent runtime break.** Old code compiles but the driver's type assertion fails and logs vanish.

```go
// v2.0.0
ctx = context.WithValue(ctx, drv.LoggerKey, slog.New(handler))
```

`DebugLevel` / `InfoLevel` / `WarnLevel` / `ErrorLevel` now alias `slog.Level`. `obs.Log(drv.ErrorLevel, "...")` call sites keep working.

### 5. Go 1.26 floor

Uses `log/slog`, `reflect.TypeFor[T]()`, and `range` over integers.

### 6. `athenareader/` is a separate module

Import path for library consumers:

```
github.com/uber/athenadriver/athenareader          (v1)
github.com/CorkCyber/athenadriver/athenareader     (v2.0.0)
```

Isolating the module drops `go.uber.org/fx`, `go.uber.org/config`, `go.uber.org/dig`, BurntSushi TOML, and `golang.org/x/{lint,tools}` from driver-only consumers.

### 7. `ServiceLimitOverride` is a typed struct

```go
// pre-v2
svc := drv.NewServiceLimitOverride()
_ = svc.SetDDLQueryTimeout(3600)
_ = svc.SetDMLQueryTimeout(1800)

// v2.0.0
svc := &drv.ServiceLimitOverride{DDLQueryTimeout: 3600, DMLQueryTimeout: 1800}
```

Removed: `NewServiceLimitOverride`, `SetDDL/DMLQueryTimeout`, `GetDDL/DMLQueryTimeout`, `GetAsStringMap`, `SetFromValues`, `ErrServiceLimitOverride`.

### 8. Constructors collapsed

| Removed | Replacement |
|---------|-------------|
| `NewDefaultObservability(cfg)` | `NewObservability(cfg, nil, nil)` |
| `NewNoOpsObservability()` | `NewObservability(NewNoOpsConfig(), nil, nil)` |
| `NewWGConfig(...)` | `&athenatypes.WorkGroupConfiguration{...}` |
| `NewNonOpsRows(...)` | *(was internal, no replacement needed)* |

### 9. Poll defaults changed

`GetQueryExecution` polling now backs off by `1.5×` up to `30s`. Long queries hit the Athena API ~5–10× less; short-query latency unchanged.

Old flat-`3s` cadence:

```go
cfg.ResultPollBackoffMultiplier = 1.0
cfg.ResultPollMaxInterval       = 3 * time.Second
```

New constants: `PollBackoffMultiplier` (1.5), `PollMaxInterval` (30s). `PoolInterval` (3s initial) unchanged.

### 10. Metrics decoupled from tally

Driver core depends only on stdlib for its metrics surface. `MetricsKey` ctx values must now satisfy `athenadriver.Scope` (2 methods: `Counter`, `Timer`) — a raw `tally.Scope` no longer type-asserts. Pick an adapter, or write your own in ~15 lines.

Ready-made:

| Backend | Module |
|---------|--------|
| OpenTelemetry | `github.com/CorkCyber/athenadriver/scope/otel` |
| tally | `github.com/CorkCyber/athenadriver/scope/tally` |
| statsd | `github.com/CorkCyber/athenadriver/scope/statsd` |

```go
import tallyscope "github.com/CorkCyber/athenadriver/scope/tally"

ctx = context.WithValue(ctx, drv.MetricsKey, tallyscope.New(rootScope))
```

## Features

Except the basic features provided by Go `database/sql` like error handling, database pool and reconnection, `athenadriver` supports the following features out of box:

- Support multiple AWS authorization methods [:link:](#support-multiple-aws-authentication-methods)
- Full support of [Athena Basic Data Types](https://docs.aws.amazon.com/athena/latest/ug/data-types.html)
- Full support of [Athena Advanced Type](https://docs.aws.amazon.com/athena/latest/ug/querying-athena-tables.html) for queries with Geospatial identifiers, ML and UDFs 
- Full support of *ALL* Athena Query Statements, including [DDL](https://docs.aws.amazon.com/athena/latest/ug/ddl-reference.html), [DML](https://docs.aws.amazon.com/athena/latest/ug/dml-queries-functions-operators.html) and [UTILITY](https://docs.aws.amazon.com/athena/latest/APIReference/API_QueryExecution.html#athena-Type-QueryExecution-StatementType) [:link:](#full-support-of-all-data-types)
- Support newly added [`INSERT INTO...VALUES`](https://aws.amazon.com/about-aws/whats-new/2019/09/amazon-athena-adds-support-inserting-data-into-table-results-of-select-query/)
- Athena workgroup and tagging support including remote workgroup creation [:link:](#query-with-workgroup-and-tag)
- Go sql's prepared statement support [:link:](#prepared-statement-support-for-athena-db)
- Go sql's `DB.Exec()` and `db.ExecContext()` support [:link:](#dbexec-and-dbexeccontext)
- Query cancelling support [:link:](#query-cancellation)
- Override default query timeout limits [:link:](#overriding-athena-service-limits-for-query-timeout)  
- Mask columns with specific values [:link:](#mask-columns-with-specific-values)
- Database missing value handling [:link:](#missing-value-handling)
- Read-Only mode - disable database write in driver level [:link:](#read-only-mode)
- Moneywise mode :moneybag: - print out query cost(USD) for each query
- Query with Athena Query ID(QID) - (the ultimate money saver! :money_with_wings: )
- Pseudo commands on the `database/sql` interface: `get_driver_version`, `get_query_id`, `get_query_id_status`, `stop_query_id` [:link:](#pseudo-commands)
- Built-in `log/slog` logging support [:link:](#enable-driver-logging)
- Bring-your-own metrics via a 2-method `Scope` interface; ready-made adapters for OpenTelemetry, tally, and statsd [:link:](#enable-metrics)
- OpenTelemetry-compatible tracing spans, tagged per the database semantic conventions so Sentry/Datadog/etc. recognize them as DB calls [:link:](#enable-tracing)

`athenadriver` can extremely simplify your code. Check [athenareader](https://github.com/CorkCyber/athenadriver/tree/main/athenareader) out as an example and a convenient tool for your Athena query in command line. 

## How to set up/install/test `athenadriver`

### Prerequisites - AWS Credentials & S3 Query Result Bucket 

To be able to query AWS Athena, you need to have an AWS account at [Amazon AWS's website](https://aws.amazon.com/). To
 give it a shot, a free
 tier account is enough. You also need to have a pair of AWS `access key ID` and `secret access key`.
You can get it from [AWS Security Credentials section of Identity and Access Management (IAM)](https://docs.aws.amazon.com/general/latest/gr/aws-security-credentials.html).
If you don't have one, please create it. The following is a screenshot from my temporary free tier account:

![How to create AWS credentials](resources/aws_keys.png)

In addition to AWS credentials, you also need an s3 bucket to store query result. Just go to 
[AWS S3 web console page](https://s3.console.aws.amazon.com/s3/home) to create one.
In the examples below, the s3 bucket I use is `s3://myqueryresults/`.

In most cases, you need the following 4 prerequisites:

- S3 Output bucket
- `access key ID`
- `secret access key`
- AWS region

For more details on `athenadriver`'s support on AWS credentials & S3 query result bucket, please refer to section
 [Support Multiple AWS Authorization Methods](#support-multiple-aws-authentication-methods).

### Installation

`athenadriver` requires Go 1.26+. Add the driver to your module:

```bash
go get github.com/CorkCyber/athenadriver/v2/go
```

To install the `athenareader` CLI tool:

```bash
go install github.com/CorkCyber/athenadriver/athenareader@latest
```

### Tests

The repository is split into six Go modules:

- `github.com/CorkCyber/athenadriver/v2` — the driver itself (`./go/...`).
- `github.com/CorkCyber/athenadriver/athenareader` — the CLI tool.
- `github.com/CorkCyber/athenadriver/examples` — runnable example
  programs, each in its own subdirectory.
- `github.com/CorkCyber/athenadriver/scope/otel`, `scope/tally`,
  `scope/statsd` — optional metrics adapters (see [Enable
  Metrics](#enable-metrics)); install only the one you use.

#### Unit tests

```bash
$ go test -race ./go/...
ok    github.com/CorkCyber/athenadriver/v2/go
```

`make test` and `make cover` are equivalent shortcuts; `make cover`
also writes `cover.html`.

#### Integration / example builds

Each example lives in its own subdirectory under `examples/` so it can
be built or run independently. From the `examples/` module:

```bash
$ cd examples
$ go build ./...                              # build everything
$ go run ./query/dml_select_simple            # run one example
```

The examples expect AWS credentials and an S3 results bucket; either
fill them into the example source or rely on the AWS SDK default
credential chain (env vars, `~/.aws/config`, IMDS, etc.).

## How to use `athenadriver`

`athenadriver` is very easy to use. What you need to do it to import it in your code and then use the standard Go `database/sql` as usual.

```go
import athenadriver "github.com/CorkCyber/athenadriver/v2/go"
```

The following are coding examples to demonstrate `athenadriver`'s features and how you should use `athenadriver` in your Go application.
Please be noted the code is for demonstration purpose only, so please follow your own coding style or best practice if necessary.

### Get Started - A Simple Query

The following is the simplest example for demonstration purpose. The source code is available at [examples/query/dml_select_simple/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/dml_select_simple/main.go).

```go
package main

import (
	"database/sql"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// Step 1. Set AWS Credential in Driver Config.
	conf, _ := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")
	// Step 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// Step 3. Query and print results
	var url string
	_ = db.QueryRow("SELECT url from sampledb.elb_logs limit 1").Scan(&url)
	println(url)
}
```

To make it work for you, please replace `OutputBucket`, `Region`, `AccessID` and
 `SecretAccessKey` with your own values. `sampledb` is provided by Amazon so you don't have to worry about it.

Build and run from the `examples/` module:

```bash
$ cd examples
$ go run ./query/dml_select_simple
https://www.example.com/articles/553
```

### Support Multiple AWS Authentication Methods

`athenadriver` resolves AWS credentials the same way any AWS SDK for Go v2 client does, via [`config.LoadDefaultConfig`](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/config#LoadDefaultConfig). There is no `AWS_SDK_LOAD_CONFIG` gate — that was a v1-only environment variable and is not read anywhere in v2. Precedence, checked in order:

1. `Config.AWSProfile` — an explicit shared-config profile name.
2. `Config.WebIdentityRoleARN` + `Config.WebIdentityTokenFile` — explicit `AssumeRoleWithWebIdentity` (EKS/IRSA-style).
3. `Config.AccessID` set explicitly on the DSN/Config — static access key, secret key, and (optionally) session token.
4. Otherwise, the standard AWS SDK v2 chain: environment variables (`AWS_ACCESS_KEY_ID`, etc.), `~/.aws/credentials` and `~/.aws/config` (including `AWS_PROFILE`), SSO, container/IRSA credentials, and IMDS.

`OutputBucket` is always required regardless of authentication method — it is not part of the AWS client config. Even with a default location set in the Athena web console, you must pass one programmatically or you will get:
`No output location provided. An output location is required either through the Workgroup result configuration setting or as an API input.`

#### Use the AWS SDK's Default Credential Chain

Leave `Config.AccessID` unset and the driver falls through to the standard chain — shared config/credentials files, SSO, container/IRSA credentials, or IMDS. This is the right choice on EC2, ECS, EKS, and Lambda, where credentials come from the execution role rather than a static key.

```go
// To use the AWS SDK's default credential chain for authentication
func useAWSCLIConfigForAuth() {
	// 1. Leave credentials unset in Driver Config.
	conf := drv.NewNoOpsConfig()
	if err := conf.SetOutputBucket(secret.OutputBucketProd); err != nil {
		println(err.Error())
		return
	}
	// 2. Open Connection.
	db, err := sql.Open(drv.DriverName, conf.Stringify())
	if err != nil {
		println(err.Error())
		return
	}
	// 3. Query and print results
	var i int
	err = db.QueryRow("SELECT 456").Scan(&i)
	if err != nil {
		println(err.Error())
	}
	println("with AWS CLI Config:", i)
}
```

If your AWS CLI setting is valid like mine, this function should output:

```go
with AWS CLI Config: 456
```

This is also the authentication method to use in [AWS Lambda](https://aws.amazon.com/lambda/): the function's execution role is picked up automatically via IMDS/container credentials, so you only need to specify the output bucket.
Please check the AWS Lambda Go sample code [here](https://github.com/CorkCyber/athenadriver/tree/main/examples/lambda/Go).

A non-default profile can be selected either via the `AWS_PROFILE` environment variable or explicitly on `Config.AWSProfile`:

```go
conf := drv.NewNoOpsConfig()
conf.AWSProfile = "profile-development" // or: os.Setenv("AWS_PROFILE", "profile-development")
```

#### Use `athenadriver` Config For Authentication

To supply a static access key explicitly, pass valid (not dummy) `accessID`, `secretAccessKey`, `region`, and `outputBucket` into `athenadriver.NewDefaultConfig()`:

```go
// To use athenadriver's Config for authentication
func useAthenaDriverConfigForAuth() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucketDev, secret.Region,
		secret.AccessID, secret.SecretAccessKey)
	if err != nil {
		return
	}
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Query and print results
	var i int
	_ = db.QueryRow("SELECT 123").Scan(&i)
	println("with AthenaDriver Config:", i)
}
```
The sample output:

```go
with AthenaDriver Config: 123
```

The full code is here at [examples/auth/main.go](https://github.com/CorkCyber/athenadriver/tree/main/examples/auth/main.go).

### Full Support of All Data Types 

As we said, `athenadriver` supports all Athena data types. 
In the following sample code, we use an SQL statement to `SELECT` som simple data of all the advanced types and then print them out.

```go
package main

import (
	"context"
	"database/sql"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")
	if err != nil {
		panic(err)
	}
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	query := "SELECT JSON '\"Hello Athena\"', " +
		"ST_POINT(-74.006801, 40.70522), " +
		"ROW(1, 2.0),  INTERVAL '2' DAY, " +
		"INTERVAL '3' MONTH, " +
		"TIME '01:02:03.456', " +
		"TIME '01:02:03.456 America/Los_Angeles', " +
		"TIMESTAMP '2001-08-22 03:04:05.321 America/Los_Angeles';"
	rows, err := db.Query(query)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	println(drv.ColsRowsToCSV(rows))
}
```

Sample output:
```bash
"Hello Athena",00 00 00 00 01 01 00 00 00 20 25 76 6d 6f 80 52 c0 18 3e 22 a6 44 5a 44 40,
{field0=1, field1=2.0},2 00:00:00.000,0-3,0000-01-01T01:02:03.456-07:52,
0000-01-01T01:02:03.456-07:52,2001-08-22T03:04:05.321-07:00
```

we can see `athenadriver` can handle all these advanced types correctly.


### Query With Workgroup and Tag 

`athenadriver` supports workgroup and tagging features of Athena. When you query Athena, you can specify the
 workgroup and tags attached with your query. Resource/cost tagging are based on workgroup. If the workgroup doesn't
 exist , by default it will be created programmatically.
 
If you want to disable programmatically creating workgroup and tags, you need to explicitly call:
```go
Config.WGRemoteCreation = false
```
In this case, you need to make sure the workgroup you specifies must exist, or you will get error. An example is like
 below:

```go
package main

import (
	"database/sql"
	"log"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, _ := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")
	wgTags := drv.NewWGTags()
	wgTags.AddTag("Uber User", "henry.wu")
	wgTags.AddTag("Uber ID", "123456")
	wgTags.AddTag("Uber Role", "SDE")
	// Specify that workgroup `henry_wu` is used for the following query
	wg := drv.NewWG("henry_wu", nil, wgTags)
	conf.SetWorkGroup(wg)
	// comment out the line below to allow remote workgroup creation and
	// the query will be successful!!!
	conf.WGRemoteCreation = false
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	rows, err := db.Query("select url from sampledb.elb_logs limit 3")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()

	var url string
	for rows.Next() {
		if err := rows.Scan(&url); err != nil {
			log.Fatal(err)
		}
		println(url)
	}
}
```

But I don't have a workgroup named `henry_wu` in AWS Athena, so I got sample output:
```go
2020/01/20 15:29:52 Workgroup henry_wu doesn't exist and workgroup remote creation
 is disabled.
```


After commenting out the `conf.WGRemoteCreation = false` line above, the output becomes:

```go
https://www.example.com/articles/553
http://www.example.com/images/501
https://www.example.com/images/183
```

and I can see a new workgroup named `henry_wu` is created in AWS Athena console: [https://us-east-2.console.aws
.amazon.com/athena/workgroups/home](https://us-east-2.console.aws.amazon.com/athena/workgroups/home)

![Athena Workgroup and Tags Automatic Creation](resources/workgroup.png)

###  Prepared Statement Support for Athena DB 

Athena doesn't support prepared statement originally. However, it could be very helpful in some 
scenarios like where part of the query is from user input. `athenadriver` supports prepared statements 
to help you to deal with those scenarios. An example is as follows:

```go
package main

import (
	"database/sql"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, _ := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Prepared Statement
	statement, err := db.Prepare("CREATE TABLE sampledb.urls AS " +
		"SELECT url FROM sampledb.elb_logs where request_ip=? limit ?")
	if err != nil {
		panic(err)
	}
	// 4. Execute prepared Statement
	if result, e := statement.Exec("244.157.42.179", 2); e == nil {
		if rowsAffected, err := result.RowsAffected(); err == nil {
			println(rowsAffected)
		}
	}
}
```

Sample output:
```go
2
```

### Parameterized Queries

Athena supports parameterized queries: https://docs.aws.amazon.com/athena/latest/ug/querying-with-prepared-statements.html.
Parameterized queries allow for re-running the same query with different parameter values at runtime, and help guard 
against SQL injection attacks. This is especially useful if some of your parameter values are derived from user input.

To use parameterized queries, use `?` as placeholders in the query you pass to `DB.Query()` or `DB.Exec()`.
For each parameter, pass in arguments in the order they should replace `?`. String and byte slice arguments are
quoted and escaped automatically — do **not** pre-format them with `drv.FormatString()`/`drv.FormatBytes()`, that
would quote them twice.

If an argument must reach Athena as a SQL *expression* rather than a value (a typecast, a function call), wrap it in
`drv.Raw` — which is unescaped, so never build one out of untrusted input:

```go
args := []any{drv.Raw("TIMESTAMP " + drv.FormatString("2024-07-01 00:00:00"))}
```

Example:

```go
query := "SELECT request_timestamp, elb_name FROM sampledb.elb_logs WHERE url=? limit 1"
rows, err := db.Query(query, "https://www.example.com/jobs/878")
if err != nil {
    return
}
println(drv.ColsRowsToCSV(rows))
```

Sample Output:
```bash
request_timestamp,elb_name
2015-01-06T04:03:01.351843Z,elb_demo_006
```


###  `DB.Exec()` and `DB.ExecContext()` 

According to Go source code, `DB.Exec()` and `DB.ExecContext()` execute a query that doesn't return rows, 
such as an `INSERT` or `UPDATE`.
It's true that you can use `DB.Exec()` and `DB.Query()` interchangeably to execute the same SQL statements.
\
However, the two methods are for different use cases and return different types of results. According to Go `database/sql` library, the result 
returned from `DB.Exec()` can tell you how many rows were affected by the query and the last inserted ID for `INSERT INTO` statement, 
which is always *-1* for Athena because auto-increment primary key feature is not supported by Athena.\
In contrast, `DB.Query()` will return a `sql.Rows` object which includes all columns and rows details.

When the only concern is if the execution is successful or not, `DB.Exec()` is 
preferred to `DB.Query()` . The best coding practice is:

```go
if _, err := DB.Exec(`<SQL_STATEMENT>`); err != nil {
    log_or_panic(err)
}
```

In cases of `INSERT INTO`, `CTAG` and `CVAS`, you may want to know when the execution 
is successful how many rows are affected by your query. Then you can use `result.RowsAffected()` as 
demonstrated in the following example:


```go
package main

import (
	"context"
	"database/sql"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	var conf *drv.Config
	var err error
	if conf, err = drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey"); err != nil {
		panic(err)
	}
	// 2. Open Connection.
	db, _ := sql.Open(drv.DriverName, conf.Stringify())
	// 3. Execute and print results
	if _, err = db.ExecContext(context.Background(),
		"DROP TABLE IF EXISTS sampledb.urls"); err != nil {
		panic(err)
	}

	var result sql.Result
	if result, err = db.Exec("CREATE TABLE sampledb.urls AS "+
		"SELECT url FROM sampledb.elb_logs where request_ip=? limit ?",
		"244.157.42.179", 1); err != nil {
		panic(err)
	}
	println(result.RowsAffected())

	if result, err = db.Exec("INSERT INTO sampledb.urls VALUES (?),(?),(?)",
		"abc", "efg", "xyz"); err != nil {
		panic(err)
	}
	println(result.RowsAffected())
	println(result.LastInsertId()) // not supported by Athena
}
```

Sample output:
```go
1
3
```

### Mask Columns with Specific Values 

Sometimes, database contains sensitive information and you may need to mask columns with specific values. If you don't
 want to display some columns, you can mask them by calling:

```go
Config.SetMaskedColumnValue("columnName", "maskValue")
```

For example, if you want to mask all rows of column `password`, you can specify:

```go
Config.SetMaskedColumnValue("password", "xxx")
```

Then all the passwords will be displayed as `xxx` in the query result set. The following is an example to
 mask column `url` in the result set.


```go
package main

import (
	"database/sql"
	"log"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, _ := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")
	conf.SetMaskedColumnValue("url", "xxx")
	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query and print results
	rows, err := db.Query("select request_timestamp, url from " +
		"sampledb.elb_logs limit 3")
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
```


Sample Output:
```go
2015-01-03T12:00:00.516940Z,xxx
2015-01-03T12:00:00.902953Z,xxx
2015-01-03T12:00:01.206255Z,xxx
```


### Query Cancellation 

AWS Athena is priced upon the data size it scanned. To save money, `athenadriver` supports query cancellation. In
 the following example, the query is cancelled if it is not complete after 2 seconds.  

```go
package main

import (
	"context"
	"database/sql"
	"log"
	"time"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, _ := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Query cancellation after 2 seconds
	ctx, _ := context.WithTimeout(context.Background(), 2*time.Second)
	rows, err := db.QueryContext(ctx, "select count(*) from sampledb.elb_logs")
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
```


Sample Output:
```go
2020/01/20 15:28:35 context deadline exceeded
```

### Overriding Athena Service Limits for Query Timeout
This library assumes default [Athena service limits](https://docs.aws.amazon.com/athena/latest/ug/service-limits.html) for DDL and DML query timeouts, as can be found in `athenadriver/go/constants.go`.
If you've increased your service limits, for example via the [Athena Service Quotas](https://console.aws.amazon.com/servicequotas/home/services/athena/quotas) console,
you can override them on your `Config`.

Here's the same example found at [Query Cancellation](#query-cancellation), but with an *increased* query timeout.

```go
package main

import (
	"context"
	"database/sql"
	"log"
	"time"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, _ := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")
	
	// 2. Override the DML query timeout to 60 minutes (3600 seconds).
	conf.ServiceLimit = &drv.ServiceLimitOverride{DMLQueryTimeout: 3600}

	// 3. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	
	// 4. Run the query.
	rows, err := db.QueryContext(context.Background(), "select count(*) from sampledb.elb_logs")
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
```

> **Per-query result encryption & reuse.** Two ctx-based per-query
> overrides: `drv.WithResultEncryption(ctx, option, kmsKey)` sets a
> one-off S3 encryption option (`athenatypes.EncryptionOption`) for that
> query's result bytes, and `drv.WithResultReuse(ctx, maxAge)` opts a
> single query into Athena's result-reuse cache (clamped to Athena's
> supported 1 minute-7 day range; `maxAge <= 0` is a no-op). Both use the
> standard `context.WithValue` pattern; pass the returned ctx to
> `QueryContext`/`ExecContext`. See `go/ctxopts.go`. `Config.ResultEncryption`
> sets the same thing driver-wide instead of per query.

### Missing Value Handling 

S3 / Athena results can have missing values. The driver lets you
choose between `empty string`, `default data`, or `nil` as the
substitute, whichever is easiest to process downstream. Defaults by
Athena type:

| Athena type | Default value |
| --- | --- |
| `tinyint`, `smallint`, `integer`, `bigint` | `0` |
| `boolean` | `false` |
| `float`, `double`, `real` | `0.0` |
| `date`, `time`, `time with time zone`, `timestamp`, `timestamp with time zone` | `time.Time{}` |
| `json`, `char`, `varchar`, `varbinary`, `row`, `string`, `binary`, `struct`, `interval year to month`, `interval day to second`, `decimal`, `ipaddress`, `array`, `map`, `unknown` | `""` |

By default, the driver uses empty string to replace missing values. When more than one of the three flags below is set, `nil` wins
over `empty string`, which wins over `default data`. To use `default data`, you have to explicitly call:

```go
Config.MissingAsEmptyString = false
Config.MissingAsDefault = true
```

If you need to use `nil` as missing value, you can call:

```go
Config.MissingAsEmptyString = false
Config.MissingAsDefault = false
Config.MissingAsNil = true
```

But if you are strict with your data integrity and want an error raised when data are missing, you can set all three of them to `false`.


### Read-Only Mode 

When read-only mode is enabled in `athenadriver`, it only allows retrieving information from Athena database.
Any writing and modification to the database will raise an error. This is useful in some cases. By default, read-only mode
is disabled. To enable it, you need to explicitly call:

```go
Config.ReadOnly = true
```

The following is one example. It enables read-only mode in line 19, but tries to create a new table with CTAS statement.
It ends up with raising an error.

```go
package main

import (
	"context"
	"database/sql"
	"log"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, _ := drv.NewDefaultConfig("s3://myqueryresults/",
		"us-east-2", "DummyAccessID", "DummySecretAccessKey")
	conf.ReadOnly = true

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)
	// 3. Create Table with CTAS statement
	rows, err := db.QueryContext(context.Background(), 
	  "CREATE TABLE sampledb.elb_logs_new AS " +
		"SELECT * FROM sampledb.elb_logs limit 10;")
	if err != nil {
		log.Fatal(err)
		return
	}
	defer rows.Close()
}
```

Sample Output:
```bash
2020/01/26 01:10:28 writing to Athena database is disallowed in read-only mode
```

### Pseudo Commands

`athenadriver` provides `pseudo command` to support some special use
cases beyond Go's standard `database/sql` framework. One example is
asynchronous query support: fire a query, get its Athena query ID
back immediately, poll for its status separately. A `pseudo command`
is a special prefix string you put in `db.QueryContext`,
`db.QueryRow`, or `db.ExecContext`.

It is easier to explain with an example like  [pc_get_query_id.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/pc_get_query_id/main.go).

```go
package main

import (
	"database/sql"
	secret "github.com/CorkCyber/athenadriver/examples/constants"
	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	// 1. Set AWS Credential in Driver Config.
	conf, err := drv.NewDefaultConfig(secret.OutputBucket, secret.Region, secret.AccessID, secret.SecretAccessKey)
	conf.LoggingEnabled = true
	if err != nil {
		panic(err)
		return
	}

	// 2. Open Connection.
	dsn := conf.Stringify()
	db, _ := sql.Open(drv.DriverName, dsn)

	// 3. Query with pseudo command `pc:get_query_id`
	var qid string
	_ = db.QueryRow("pc:get_query_id select url from sampledb.elb_logs limit 2").Scan(&qid)
	println("Query ID: ", qid)
}
```

In [pc_get_query_id/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/pc_get_query_id/main.go), we only want the `Query ID` of the SQL statement, so we prepend `pc:get_query_id` to the SQL. So the final string we pass to `db.QueryRow` is `pc:get_query_id select url from sampledb.elb_logs limit 2`. The return value is one row with an Athena Query ID inside. A sample Output is:
```
Query ID: c89088ab-595d-4ee6-a9ce-73b55aeb8953
```

Now we support three pseudo commands: `get_query_id`, `get_query_id_status`, `stop_query_id`.

The syntax is `pc:pseudo_command parameter`.

### get_query_id

`pc:get_query_id SQL_STATEMENT` - Will return Query ID of the `SQL_STATEMENT`, no matter request fails or succeeds. Example: [pc_get_query_id.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/pc_get_query_id/main.go).

### get_query_id_status

`pc:get_query_id_status Query_ID` - Return status of the Query ID. Example: [pc_get_query_id_status.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/pc_get_query_id_status/main.go).

### stop_query_id

`pc:stop_query_id Query_ID` - To stop the Query corresponding the Query ID. If there is no error, a one row string with `OK` will be returned. Example: [pc_stop_query_id.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/pc_stop_query_id/main.go).

### get_driver_version

`pc:get_driver_version` - To return the version of athenadriver. Example: [pc_get_driver_version.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/pc_get_driver_version/main.go).


###  Enable Driver Logging

The driver logs through the standard library's `log/slog`. By default it
is silent: every record is routed to an internal discard handler, so
importing the package does not emit anything until you opt in. The driver
intentionally does not consult `slog.Default()` — apps that set a global
default handler will not see driver logs spill into it unless they
explicitly opt in.

#### Wiring a logger

Attach a `*slog.Logger` to the `context.Context` you hand to
`db.QueryContext` / `db.ExecContext` under the `LoggerKey` value:

```go
package main

import (
	"context"
	"database/sql"
	"log"
	"log/slog"
	"os"
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
)

func main() {
	conf, _ := drv.NewDefaultConfig("s3://query-results-bucket-test/",
		"us-east-2",
		"dummy-to-be-replaced",
		"dummy-to-be-replaced")
	db, _ := sql.Open(drv.DriverName, conf.Stringify())

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, drv.LoggerKey, logger)

	rows, err := db.QueryContext(ctx, "select count(*) from sampledb.elb_logs")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
}
```

The logger is read once per pooled connection, in `SQLConnector.Connect`.
The first context that opens a pooled conn wins; subsequent queries on
that same conn use the logger that was installed at open time. If you
need per-query logger swapping, use separate `*sql.DB` instances.

#### Opting out

| Goal | How |
| --- | --- |
| Default (no logs at all) | Do nothing. The driver wires `slog.New(slog.NewTextHandler(io.Discard, nil))` automatically. |
| Pass a no-op handler explicitly | `slog.New(slog.DiscardHandler)` or `slog.New(slog.NewTextHandler(io.Discard, nil))`. |
| Hard kill-switch | `conf.LoggingEnabled = false`. Short-circuits inside `DriverTracer.Log` before attrs are formatted; `DriverTracer.Logger()` returns the discard logger regardless of any context-supplied logger. |

Sample output (with an `slog.NewJSONHandler` and Info-level threshold):

```json
{"time":"2026-06-15T13:44:26Z","level":"WARN","msg":"query canceled","queryID":"ef4f3f09-a480-445c-84ad-96ecd97a8e90"}
```

### Enable Metrics

The driver emits counters + timers through three small interfaces
(`athenadriver.Scope`, `Counter`, `Timer`) — no vendored metrics
library. `Scope` has 2 methods (`Counter(name) Counter`, `Timer(name)
Timer`); `Counter` and `Timer` each have 1 (`Inc(int64)`,
`Record(time.Duration)`). Bring your own backend by supplying a
`Scope`. Metrics are enabled by default but wired to `NoopScope`, so
nothing is emitted until you inject one:

```go
conf.MetricsEnabled = true // (already the default)
connector := drv.NewConnector(conf).WithScope(myScope)
db := sql.OpenDB(connector)
```

`WithScope` (like `WithTracer` below) applies deterministically to every
connection this connector produces. A `MetricsKey` ctx-value form also
exists (passed to `sql.Conn`'s underlying `Connect`), but — unlike
`TracerKey` — it is not re-checked per query, and which pooled
connections pick it up depends on `database/sql`'s internal pool
mechanics; see [Enable Tracing](#enable-tracing) for the full
explanation. Prefer `WithScope`.

Turn metrics fully off with `conf.MetricsEnabled = false`.

**Ready-made adapters** (each a separate module — install only what
you use):

| Backend | Module | Constructor |
|---------|--------|-------------|
| OpenTelemetry | `github.com/CorkCyber/athenadriver/scope/otel`  | `otelscope.New(metric.Meter)` |
| tally   | `github.com/CorkCyber/athenadriver/scope/tally`  | `tallyscope.New(tally.Scope)` |
| statsd  | `github.com/CorkCyber/athenadriver/scope/statsd` | `statsdscope.New(statsd.Statter)` |

Rolling your own backend is ~15 lines of glue — `Scope` itself only
has two methods; `Counter.Inc(int64)` and `Timer.Record(time.Duration)`
are each a single-method interface. See any of the three adapter
packages for a reference.

**Example — OpenTelemetry** (see `examples/metrics/` for the full
runnable version with an OTLP-shaped exporter):

```go
import (
    "go.opentelemetry.io/otel"
    drv       "github.com/CorkCyber/athenadriver/v2/go"
    otelscope "github.com/CorkCyber/athenadriver/scope/otel"
)

ctx = context.WithValue(ctx, drv.MetricsKey,
    otelscope.New(otel.Meter("athenadriver")))
```

Metric names all start with `awsathena.` — e.g.
`awsathena.connector.connect`, `awsathena.query.startqueryexecution`.

### Enable Tracing

The driver starts one span per `QueryContext`/`ExecContext` call through
two small interfaces (`athenadriver.Tracer`, `Span`) — the same
bring-your-own-backend shape as `Scope`. Tracing is enabled by default
but wired to `NoopTracer`, so nothing is emitted until you inject one:

```go
conf.TracingEnabled = true // (already the default)
connector := drv.NewConnector(conf).WithTracer(myTracer)
db := sql.OpenDB(connector)
```

`WithTracer` is the deterministic wiring: it applies to every connection
this connector ever produces. A ctx-value form also exists
(`context.WithValue(ctx, drv.TracerKey, myTracer)`, passed to
`db.QueryContext`/`db.ExecContext`) for a **per-query override** — a
request-scoped sampling decision, say — layered on top of whatever
`WithTracer` set. Don't rely on the ctx-value form as your *only* wiring:
`database/sql` pools connections, and a connection already established
before your ctx carries a value will not pick it up (see `WithTracer`'s
doc comment for why).

Turn tracing fully off with `conf.TracingEnabled = false`.

Every span is tagged as a database client call using [OpenTelemetry's
database semantic
conventions](https://opentelemetry.io/docs/specs/semconv/db/database-spans/)
(`db.system.name=aws.athena`, `db.namespace`, `db.operation.name`,
`server.address`) plus `athena.query_id` / `athena.workgroup` /
`athena.catalog` / `athena.statement_type` / `athena.data_scanned_bytes` —
a backend that understands those conventions (Sentry, Datadog APM,
Honeycomb, Tempo, ...) renders it as a database call, not a generic span.
`db.query.text` is deliberately not attached: the DDL-interpolation path
embeds literal argument values in the query text, and a trace is not
where those should land by default.

**OpenTelemetry adapter** (`github.com/CorkCyber/athenadriver/scope/otel`
— the same module as the metrics adapter):

```go
import (
    "go.opentelemetry.io/otel"
    drv       "github.com/CorkCyber/athenadriver/v2/go"
    otelscope "github.com/CorkCyber/athenadriver/scope/otel"
)

connector := drv.NewConnector(conf).
    WithTracer(otelscope.NewTracer(otel.Tracer("athenadriver")))
```

Rolling your own backend is a couple dozen lines of glue — `Tracer` has
one method (`StartSpan`), `Span` has three (`SetAttr`, `RecordError`,
`End`). See `scope/otel`'s `NewTracer`/`tracerAdapter`/`spanAdapter` for a
reference. Implementations must be safe for concurrent use: one `Tracer`
is shared across every pooled connection.

## Limitations of Go/Athena SDK's and `athenadriver`'s Solution

### Column number mismatch in `GetQueryResults` of Athena Go SDK

#### `ColumnInfo` has more number of cloumns than `Rows[0].Data`

> ![](resources/pin.png)**Affected Statements: DESCRIBE TABLE/VIEW, SHOW SCHEMA/TABLE/...**

- Sample Query:

```sql
DESC sampledb.elb_logs
```

- Analysis:

![Column number mismatch issue example 1](resources/issue_1.png)

We can see there are 3 columns according to `ColumnInfo` under `ResultSetMetadata`. But in the first row `Rows[0]`, we see there is only 1 field: `"elb_name \tstring    \t    "`. I would imagine there could have been 3 items in the `Data[0]`, but somehow the code author doesn't split it with tab(`\t`), so it ends up with only 1 item. The same issue happens for `SHOW` statement.

For more sample code, see [util_desc_table/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/util_desc_table/main.go), [util_desc_view/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/util_desc_view/main.go), and [util_show/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/util_show/main.go).

- `athenadriver`'s Solution:

`athenadriver` fixes this issue by splitting `Rows[0].Data[0]` string with tab, and replace the original row with a new row which has the same number of data with columns.

#### `ColumnInfo` has cloumns but `Rows` are empty

> ![](resources/pin.png)**Affected Statements: [`CTAS`](https://docs.aws.amazon.com/athena/latest/ug/ctas.html), CVAS, INSERT INTO**

Sample Query:

```sql
CREATE TABLE sampledb.elb_logs_copy WITH (
    format = 'TEXTFILE',
    external_location = 's3://external-location-test/elb_logs_copy', 
    partitioned_by = ARRAY['ssl_protocol'])
AS SELECT * FROM sampledb.elb_logs
```

Analysis:

![Column number mismatch issue example 2](resources/issue_3.png)

In the above [`CTAS`](https://docs.aws.amazon.com/athena/latest/ug/ctas.html) statement, we see there is one column of type `bigint` named
 `"rows"` in the resultset, but `ResultSet.Rows` is empty. Since there is no
  row, that one column doesn't make sense, or at least is confusing. The same
  issue happens for `INSERT INTO` statement.

- `athenadriver`'s Solution:

Because this issue happens only in statements [`CTAS`](https://docs.aws.amazon.com/athena/latest/ug/ctas.html), `CVAS`, and `INSERT INTO
`, where `UpdateCount` is always valid and is the only meaningful information
 returned from Athena, `athenadriver` sets `UpdateCount` as the value of
  the returned row.

For more sample code, see [ddl_ctas/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/ddl_ctas/main.go), [ddl_cvas/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/ddl_cvas/main.go), and [dml_insert_into_select/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/dml_insert_into_select/main.go).

### Type Loss for map, struct, array etc

One of Athena Go SDK's limitations is the type information could be lost after 
querying. I think there are two reasons for this type information loss.

The first reason is Athena SDK doesn't provide the full type information for complex type data.
It assumes the application developers know the data schema and should take the responsibility of data serialization.

To dig into the code, all query results are stored in data structure 
[`ResultSet`](https://docs.aws.amazon.com/athena/latest/APIReference/API_ResultSet.html).
From the UML class graph of `ResultSet` below, we can see the type 
information are stored in `ColumnInfo`'s pointer to string variable `Type`, 
which is only a type name of data type, not containing any type metadata. 
For example, querying a map of `string->boolean` will return the type name `map`, 
but you cannot find the information `string->boolean` in the `ResultSet`. For simple type like `integer`, 
`boolean` or `string`, it is sufficient to serialize them to Go type, but for more complex types like `array`, 
`struct`, `map` or nested types, the type information is lost here.

![UML class graph of `ResultSet`](resources/ResultSet_Uml.png)

The second reason is the difference between Athena data type and Go data type. 
Some Athena builtin data type like `Row`, `DECIMAL(p, s)`, `varbinary`, `interval year to month`
are not supported in Go standard library. Therefore, there is no way to serialize them in driver level.

- `athenadriver`'s Solution:

For data types: `array`, `map`, `json`, `char`, `varchar`, `varbinary`, `row`, `string`, `binary`, `struct`, `interval year to month`, `interval day to second`, `decimal`, `athenadriver` returns the string representation of the data. The developers can firstly retrieve the string representation, and then serialize to user defined type on their own.

For time and date types: `date`, `time`, `time with time zone`, `timestamp`, `timestamp with time zone`, `athenadriver` returns Go's [`time.Time`](https://golang.org/pkg/time/#Time).

Sample code: [dml_select_array/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/dml_select_array/main.go),
[dml_select_map/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/dml_select_map/main.go), [dml_select_time/main.go](https://github.com/CorkCyber/athenadriver/blob/main/examples/query/dml_select_time/main.go).


## FAQ

The following is a collection of questions from our software developers and data scientists.

### Does `athenadriver` support database reconnection?

Yes. `database/sql` maintains a connection pool internally and handles connection pooling, reconnecting, and retry logic for you.
One pitfall of writing Go sql application is cluttering the code with error-handling and retry.
I tested in my application with `athenadriver` by turning off and on Wifi and VPN, it works very well with database reconnection.

### Does `athenadriver` support batched query?
  
No. `athenadriver` is an implementation of `sql.driver` in Go `database/sql`, where there is no batch query support.
There might be some workaround for some specific case though. For instance, 
if you want to insert many rows, you can use [db.Exec](https://golang.org/pkg/database/sql/#DB.Exec) 
by replacing multiple inserts with one insert and multiple VALUES.
 
### How to use `athenadriver` to get total row number of result set?

You have to use `rows.Next()` to iterate all rows and use a counter to get row number. It is because Go `database/sql` was designed in a streaming query way with big data considered. That is why it only supports using `Next()` to iterate. So there is no way for random access of row. In Athena case, we only have random access of all the rows within one result page as the picture shown below:

![Encapsulation of driver.Rows in sql.Rows](resources/sql_Rows.png) 

But due to encapsulation, more sepcifically the `rowsi` is _private_, we
 cannot access it directly like when we using Athena Go SDK. We have to use `Next()` to access it one by one.

### Is there any way to randomly access row with `athenadriver`?

No. The reason is the same as answer to the previous question.

### Does `athenadriver` support getting the rows affected by my query?
  
To put it simple, YES. But there is some limitation and best practice to follow.
  
The recommended way is to use `DB.Exec()` to get it. Please refer to [ :link: ](#dbexec-and-dbexeccontext).

You can get it with `DB.Query()` too. In the returned `ResultSet`, there is
 an `UpdateCount` member variable. If the query is one of [`CTAS`](https://docs.aws.amazon.com/athena/latest/ug/ctas.html), `CVAS` and `INSERT INTO`, `UpdateCount` will contain meaningful value. The result will be of a one row and one column. The column name is `rows`, and the row is an `int`, which is exactly `UpdateCount`. I would suggest to use `QueryRow` or `QueryRowContext` since it is a one-row result. By the way, the document for [`GetQueryResults`](https://docs.aws.amazon.com/athena/latest/APIReference/API_GetQueryResults.html) seems not very accurate.

![UpdateCount for CTAS, VTAS, and INSERT INTO](resources/issue_2.png)

In practice, not only [`CTAS`](https://docs.aws.amazon.com/athena/latest/ug/ctas.html) statement but also `CVAS` and `INSERT INTO` will make a meaningful `UpdateCount`.

## Development Status

This fork is preparing the v2.0.0 release, covering the `aws-sdk-go-v2`
migration, the `log/slog` switch, and the Athena API additions listed
in [CHANGELOG.md](CHANGELOG.md). The public API surface is intended to
follow [SemVer](http://semver.org/) once tagged.

## Contributing

PRs and issues welcome. See [resources/CONTRIBUTING.md](resources/CONTRIBUTING.md)
and the [code of conduct](resources/CODE_OF_CONDUCT.md). Contributions
that originally targeted `uber/athenadriver` or `grafana/athenadriver`
are good candidates to re-open against this repo.


### `athenadriver` UML Class Diagram

For the contributors, the following is `athenadriver` Package's UML Class Diagram which may help you to
 understand the code. You can also check the reference section below for some useful materials. 


![`athenadriver` Package's UML Class Diagram](resources/athenadriver.png)


## Reference 

- [Amazon Athena User Guide](https://docs.aws.amazon.com/athena/latest/ug/what-is.html)
- [Amazon Athena API Reference - Describes the Athena API operations in detail.](https://docs.aws.amazon.com/athena/latest/APIReference/Welcome.html)
- [Amazon Athena Go Doc](https://godoc.org/github.com/aws/aws-sdk-go-v2/service/athena)
- [Data type mappings that the JDBC driver supports between Athena, JDBC, and Java](https://s3.amazonaws.com/athena-downloads/drivers/JDBC/SimbaAthenaJDBC_2.0.5/docs/Simba+Athena+JDBC+Driver+Install+and+Configuration+Guide.pdf#page=37)
- [Service Quotas](https://docs.aws.amazon.com/athena/latest/ug/service-limits.html)
- [Go sql connection pool](http://go-database-sql.org/connection-pool.html)
- [Common Pitfalls When Using database/sql in Go](https://www.vividcortex.com/blog/2015/09/22/common-pitfalls-go/)
- [Implement Sql Database Driver in 100 Lines of Go](https://vyskocil.org/blog/implement-sql-database-driver-in-100-lines-of-go/)

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the full release history.
[`grafana/athenadriver`](https://github.com/grafana/athenadriver) and
[`uber/athenadriver`](https://github.com/uber/athenadriver) hold the
pre-fork history.


----

💡 `athenadriver` and `athenareader` were created by [Henry Fuheng Wu](mailto:wufuheng@gmail.com) at [Uber Technologies](https://en.wikipedia.org/wiki/Uber), carried forward through the `aws-sdk-go-v2` port by [Grafana Labs](https://github.com/grafana), and are now maintained by [Cork Cyber](https://github.com/CorkCyber).


[doc-img]: https://img.shields.io/badge/GoDoc-Reference-red.svg
[doc]: https://pkg.go.dev/mod/github.com/CorkCyber/athenadriver


[release-img]: https://img.shields.io/github/v/tag/CorkCyber/athenadriver?label=release
[release]: https://github.com/CorkCyber/athenadriver/releases

[report-card-img]: https://goreportcard.com/badge/github.com/CorkCyber/athenadriver
[report-card]: https://goreportcard.com/report/github.com/CorkCyber/athenadriver

[license-img]: https://img.shields.io/badge/License-MIT-red
[license]: https://github.com/CorkCyber/athenadriver/blob/main/LICENSE

[release-policy]: https://golang.org/doc/devel/release.html#policy

[made-img]: https://img.shields.io/badge/Maintained%20by-Cork%20Cyber-purple
[made]: https://github.com/CorkCyber

