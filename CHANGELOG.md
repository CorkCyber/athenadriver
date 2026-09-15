# Changelog

All notable changes to this driver are documented here. Format roughly follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_Nothing yet. v2.0.0 is the current target; see below._

## [2.0.0] — aws-sdk-go-v2 + typed Config (`CorkCyber/athenadriver`)

First release under `github.com/CorkCyber/athenadriver/v2`. Fork chain:
[uber](https://github.com/uber/athenadriver) (dormant since 2025-05-31) →
[grafana](https://github.com/grafana/athenadriver) (carried the v1 → v2
AWS SDK port) → this repo.

### Breaking

Full migration steps live in the [README v2 migration guide](README.md#v200-migration-guide).

1. **Module path** — `uber/athenadriver` → `CorkCyber/athenadriver/v2` (Go semantic import versioning for a v2+ module). Import the driver as `github.com/CorkCyber/athenadriver/v2/go`. `drv.DriverName` unchanged.
2. **AWS SDK v1 → v2** — types move to `aws-sdk-go-v2/service/athena/types`; credentials flow through `aws.Config`. DSN keys unchanged.
3. **`Config` is a typed struct** — assign fields directly. Validating setters kept: `SetOutputBucket`, `SetRegion`, `SetAccessID`, `SetSecretAccessKey`, `SetWorkGroup`.
4. **Logger** — `*zap.Logger` → `*slog.Logger`. Silent runtime break for callers that inject via `LoggerKey` ctx.
5. **Go floor** — 1.13 → 1.24.
6. **`athenareader/` is a separate module** — `github.com/CorkCyber/athenadriver/athenareader`.
7. **`ServiceLimitOverride` is a typed struct** — assign `DDLQueryTimeout` / `DMLQueryTimeout` fields. Setters + `ErrServiceLimitOverride` removed.
8. **Constructors collapsed** — `NewDefaultObservability`, `NewNoOpsObservability`, `NewWGConfig`, `NewNonOpsRows` removed. Use `NewObservability(cfg, nil, nil)` and struct literals.
9. **Poll defaults** — backoff `1.5×` (was `1.0`), cap `30s` (was equal to initial interval). New constants: `PollBackoffMultiplier`, `PollMaxInterval`. Opt out via `Config.ResultPollBackoffMultiplier` and `Config.ResultPollMaxInterval`.
10. **Metrics decoupled from tally.** Driver depends only on stdlib for its metrics surface (`Scope` / `Counter` / `Timer` interfaces + `NoopScope`). `tally.Scope` in a `MetricsKey` ctx no longer type-asserts. Bridge via one of the shipped adapter modules or a ~15-line custom one:
    - `github.com/CorkCyber/athenadriver/scope/otel`
    - `github.com/CorkCyber/athenadriver/scope/tally`
    - `github.com/CorkCyber/athenadriver/scope/statsd`
11. **String / `[]byte` query arguments are now quoted and escaped automatically.** `buildExecutionParams` previously passed them to Athena's `ExecutionParameters` unquoted — a SQL injection primitive, since Athena evaluates each parameter as a SQL expression rather than a bound value. Existing code that pre-formats an argument with `drv.FormatString()` / `drv.FormatBytes()` before passing it to `Query`/`Exec` will now double-quote it. To send a raw, unquoted SQL expression (a typecast, a function call), wrap it in the new `drv.Raw` type instead — never build one from untrusted input. See [Parameterized Queries](README.md#parameterized-queries).
12. **`Config.String()` now returns a credential-masked DSN**, not the full connectable one — it previously leaked the AWS secret key and session token in cleartext to any incidental `%v`/`%+v` formatting. Code relying on `String()` to produce a DSN usable with `sql.Open` must call `Stringify()` explicitly instead.

### Added

**Credentials & connectivity**
- `SQLConnector.WithAWSConfig(aws.Config)` + `NewConnector(cfg)` — inject pre-built `aws.Config`. Unlocks IMDS, IRSA, SSO, OIDC, custom retryers.
- `Config.AWSProfile` (DSN: `AWSProfile=`) works without `AWS_SDK_LOAD_CONFIG=true`.
- AssumeRoleWithWebIdentity via `Config.WebIdentityRoleARN` / `WebIdentityTokenFile` / `WebIdentityRoleSessionName` (or equivalent DSN keys).

**Query execution**
- `Statement.ExecContext` / `QueryContext` (`driver.Stmt*Context`) — prepared statements honor caller context.
- `Connection.ResetSession` / `IsValid` — `database/sql` discards cancelled/closed conns cleanly.
- `ClientRequestToken` on every `StartQueryExecution`; default fresh UUIDv4 (idempotent SDK retries). Override via `ClientRequestTokenKey` ctx.
- Statement-aware parameter routing: `?` placeholders go to Athena as `ExecutionParameters` for SELECT / INSERT / CTAS / UNLOAD / WITH; parameterized DDL (ALTER, MSCK, plain CREATE, ...) is interpolated client-side — Athena rejects `ExecutionParameters` on DDL.
- `QueryFailureError` — FAILED queries surface Athena's structured `AthenaError` (`ErrorCategory` / `ErrorType` / `Retryable`) via `errors.As`, so callers can tell retryable system errors from user SQL errors.
- `Catalog` on `QueryExecutionContext`. `Config.Catalog` + `CatalogOrDefault()` + `DefaultCatalog` constant. Per-query override via `CatalogKey` ctx.
- `ResultReuseConfiguration` opt-in via `WithResultReuse(ctx, maxAge)` (`ResultReuseMaxAgeKey`); clamped to `[1, 60]` min.

**Results & security**
- `Rows.StatementType()`, `Rows.SubstatementType()`, `Rows.QueryID()`.
- `RowsColumnTypeScanType`, `RowsColumnTypeNullable`, `RowsColumnTypePrecisionScale`.
- `Config.ResultEncryption` (SSE-S3 / SSE-KMS / CSE-KMS). Per-query override via `WithResultEncryption` or `ResultEncryptionKey` ctx.
- `Config.ExpectedBucketOwner` (cross-account safety). Per-query override via `ExpectedBucketOwnerKey` ctx.
- `SQLDriver.Validate(dsn)` — startup-time DSN check; also implements `driver.Validator.IsValid()`.

**Poll tuning**
- `Config.ResultPollBackoffMultiplier` / `Config.ResultPollMaxInterval`.

### Fixed

**Correctness / contract**
- `Statement.Close` idempotent; `Exec` / `Query` / `*Context` no longer auto-close so `stmt.Query(a); stmt.Query(b)` works.
- `SQLConnector.Connect` no longer mutates a shared tracer field — was a data race across goroutines that swapped logger/scope on live Connections. Each Connection now owns its tracer.
- `NewConnector` copies the Config at construction — mutating the caller's `*Config` after `sql.OpenDB` no longer races with (or retroactively changes) pooled connections.
- Boolean query args emit Trino literals `true` / `false` (were MySQL-style `1` / `0`, which fail Athena BOOLEAN type-checks).
- Poll loop no longer panics on `FAILED` state with nil `StateChangeReason`; surfaces generic error.
- `buildExecutionParams` returns `nil` (not empty slice) so `StartQueryExecution` omits `ExecutionParameters` for non-parameterized queries.
- Server-side query stopped when caller ctx is cancelled mid-`GetQueryExecution` (was leaking billed scan).

**Scanning**
- Athena `TIMESTAMP` values without fractional seconds now scan (`"2006-01-02 15:04:05"` layout was missing) — and parse on the first attempt via shape-dispatched layout selection instead of failing through up to five layouts per cell (~9x faster on the common shape; matters on million-row scans).

**DSN round-trip**
- `Config.WGRemoteCreation = false` survives round-trip.
- Custom `WorkGroupConfiguration` on a Workgroup survives round-trip (pre-v2 path silently swapped in `GetDefaultWGConfig()`).
- Invalid integers in DSN keys (`resultPollIntervalSeconds`, `resultPollMaxIntervalSeconds`, `resultPollBackoffMultiplier`, `DDLQueryTimeout`, `DMLQueryTimeout`) error at `NewConfig` instead of silently defaulting.
- Explicit `ResultPollBackoffMultiplier = 1.0` ("disable backoff") survives the round-trip instead of silently reverting to the 1.5 default.
- `missingAsEmptyString` and `MetricsEnabled` default to **true** when absent from the DSN, matching `NewNoOpsConfig` and the pre-v2 defaults (metrics stay wired to `NoopScope` until a `Scope` is injected via `MetricsKey`).
- Bucket host with whitespace / non-DNS-safe characters, and single-slash or multi-slash DSNs (`s3:/foo`, `s3://host//path`), now error at `NewConfig`; previously parsed but failed on Stringify → re-parse (found via `FuzzNewConfig`).

**Query validation**
- Query length gates on placeholder-form (what Athena receives against the 262 KiB cap), not on the interpolated string.

**Tooling**
- Mock `aws/transport/http.ResponseError` literal keyed; `go vet` clean, CI drops `-vet=off`.

### Changed

Internal only. See "Breaking" above for user-visible v1 → v2 changes.

- Driver no longer pulls `go.uber.org/zap`, `zapcore`, or `go.uber.org/multierr`. Only remaining uber transitive: `go.uber.org/atomic` (required by `tally/v4`).
- `(*Rows).fetchNextPage` uses `athena.NewGetQueryResultsPaginator`; hand-rolled `NextToken` loop gone.
- Vendored `aws-sdk-go` v1 `awsutil.Prettify` replaced with a purpose-built formatter for `athena/types.WorkGroupConfiguration` and its nested types.
- CI replaced `.travis.yml` with `.github/workflows/ci.yml`: `go vet`, `gofmt -s`, `go test -race` across Go 1.22 / 1.23 / 1.24, Codecov, athenareader CLI build.
- `go.mod` `go` directive relaxed from `1.26.3` (Grafana plugin default) to `1.22`.
- `examples/metrics/main.go` rewritten against the new `Scope` interface, using the OpenTelemetry adapter (`scope/otel`) as the reference implementation; `scope/tally` and `scope/statsd` are noted as drop-in alternatives.
- README: "About this fork" section, badges refreshed, FOSSA badge dropped.

### Removed

- Self-import of `github.com/uber/athenadriver` from `go.mod`.
- Indirect deps dropped after import rewrite: `aws-sdk-go` v1, `jmespath`, `uber-go/tally` v3.

### Upstream fixes carried forward (`uber/athenadriver`)

- Underlying AWS error passed through workgroup fetch (PRs #64, #68, #70).
- Nil-pointer fix on workgroup tags (PR #69).
- `ExecutionParameters` nil when no args (PR #74).
- `tally` v3 → v4 (PR #76).
- Parameterized-query README docs.

[Unreleased]: https://github.com/CorkCyber/athenadriver/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/CorkCyber/athenadriver/releases/tag/v2.0.0

---

## Pre-fork history (`uber/athenadriver` v1.x)

The driver's pre-`grafana/main` history. Preserved verbatim from the
`ChangeLog.txt` shipped by Uber Technologies, reformatted as Markdown.

### v1.1.15 — Merge community contribution (March 03, 2024)

- Rename S3 bucket in test code (@jonathanbaker7 Jonathan Baker, @henrywoo)
- Make poll interval configurable (@keshav-dataco Keshav Murthy)
- Add microseconds and nanosecond time format parsing (@Sly1024 Szilveszter Safar)
- Add option to return missing values as nil (@kevinwcyu Kevin Yu)

### v1.1.14 — Merge community contribution (August 19, 2022)

- Adding default AWS SDK credential resolution to connector (dfreiman-hbo, Dan Freiman)
- Bump go-pretty version to most recent version (nyergler, Nathan Yergler)
- Expose DriverTracer factory functions (andresmgot, Andres Martinez Gotor)
- Add support to go 1.17+ (henrywoo, Henry Fuheng Wu)
- README cleanup (henrywoo, Henry Fuheng Wu)

### v1.1.13 — Merge community contribution (July 16, 2021)

- Overriding Athena Service Limits for Query Timeout
- README cleanup

### v1.1.12 — Minor bug fix and more documentation (October 29, 2020)

- Use exact match for Query ID search

### v1.1.11 — Minor bug fix and more documentation (June 16, 2020)

- Uber Engdoc documentation
- Support `$path` in Athena query
- Remove SQL Tidy function and working on replacing it with a Presto SQL parser
  in the future

### v1.1.10 — Minor bug fix and more documentation (June 5, 2020)

- documentation and minor bug fix

### v1.1.8 — Athenareader output style and format added (May 31, 2020)

- prettify athenareader output
- One bug fix (https://github.com/uber/athenadriver/issues/12)

### v1.1.6 — Pseudo commands, bug fix and more document and sample code (May 25, 2020)

- Introduce pseudo commands: `get_query_id`, `get_query_id_status`,
  `stop_query_id`, `get_driver_version`
  (doc: https://github.com/uber/athenadriver#pseudo-commands;
  sample code: https://github.com/uber/athenadriver/tree/master/examples)
- Enable AWS profile manual setup for authentication
  (sample code: https://github.com/uber/athenadriver/blob/master/examples/auth.go)
- Query Athena with athenadriver in AWS Lambda
  (https://github.com/uber/athenadriver/tree/master/examples/lambda/Go)
- One bug fix (https://github.com/uber/athenadriver/commit/8618706818a8db7abc8f1bd344ac0eca50d38959)
