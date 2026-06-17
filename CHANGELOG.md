# Changelog

All notable changes to this driver are documented here. Format roughly follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `database/sql` column metadata: `RowsColumnTypeScanType`,
  `RowsColumnTypeNullable`, `RowsColumnTypePrecisionScale` on `*Rows`.
- `Connection.ResetSession(ctx)` (implements `driver.SessionResetter`)
  and `Connection.IsValid()` (implements `driver.Validator`). Lets
  database/sql discard cancelled / closed connections cleanly instead
  of handing them back out of the pool.
- AssumeRoleWithWebIdentity via DSN:
  `Config.SetWebIdentity(roleARN, tokenFile, sessionName)` (and the
  DSN keys `webIdentityRoleARN`, `webIdentityTokenFile`,
  `webIdentityRoleSessionName`). `resolveAWSConfig` builds a
  `stscreds.WebIdentityRoleProvider` wrapped in `aws.CredentialsCache`
  when both `roleARN` and `tokenFile` are set.
- `SQLConnector.WithAWSConfig(awsCfg aws.Config)` and a
  `NewConnector(cfg)` constructor that lets callers inject a pre-built
  `aws.Config`. Unlocks IMDS, IRSA / EKS pod identity, SSO, OIDC,
  custom credential providers, custom retryers, and any other
  aws-sdk-go-v2 configuration the driver does not surface
  individually.
- `Config.SetAWSProfile(name)` (and the equivalent `AWSProfile=` DSN
  key) now takes effect without requiring `AWS_SDK_LOAD_CONFIG=true`.
  When a profile is set, the driver invokes
  `config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(name))`
  directly, picking up `~/.aws/config` profiles, SSO sessions, and
  assume-role chains.
- `Config.SetResultPollBackoff(multiplier, maxInterval)` adds
  exponential backoff between `GetQueryExecution` polls. Default is
  `multiplier=1.0` (no backoff, behavior unchanged).
- `EncryptionConfiguration` on `ResultConfiguration` (SSE-S3 / SSE-KMS /
  CSE-KMS for result S3). `Config.SetResultEncryption(option, kmsKey)` /
  `GetResultEncryption()` for a connection-wide default; per-query
  override via `WithResultEncryption(ctx, option, kmsKey)` or
  `ResultEncryptionKey` ctx value.
- `ExpectedBucketOwner` on `ResultConfiguration` (cross-account safety
  check). `Config.SetExpectedBucketOwner` / `GetExpectedBucketOwner`;
  per-query override via `ExpectedBucketOwnerKey` ctx value.
- `Rows.StatementType()`, `Rows.SubstatementType()`, `Rows.QueryID()`
  accessors that expose Athena's classification of the query (DML /
  DDL / SELECT / CREATE_TABLE_AS_SELECT / etc.). Populated from the
  terminal `GetQueryExecution` response captured by the polling loop.
- `SQLDriver.Validate(dsn string) error` — DSN sanity check that returns
  the same parse error `Open` would, suitable for startup-time DSN
  validation without opening a connection. Also implements
  `driver.Validator.IsValid()` (always `true`; Athena connections have
  no persistent server-side state).
- `Statement.ExecContext` / `Statement.QueryContext` (`driver.StmtExecContext`
  / `driver.StmtQueryContext`) so prepared-statement execution honors caller
  context cancellation instead of falling back to `context.Background()`.
- `ClientRequestToken` on every `StartQueryExecution`. Defaults to a fresh
  UUIDv4 per query (via `crypto/rand`) so AWS SDK retries are idempotent and
  a transient network blip does not produce a duplicate, double-charged
  query. Per-query override via `context.WithValue(ctx,
  athenadriver.ClientRequestTokenKey, "<token>")`.
- `ResultReuseConfiguration` (Athena engine v3 result cache) opt-in via
  `athenadriver.WithResultReuse(ctx, maxAge)` (or `ResultReuseMaxAgeKey`).
  `maxAge` is clamped to Athena's `[1, 60]` minutes range; queries that
  hit the cache skip the scan entirely and are not billed.
- `Catalog` on `QueryExecutionContext`. Defaults to `Config.GetCatalog()`
  (which defaults to `AwsDataCatalog`); per-query override via
  `context.WithValue(ctx, athenadriver.CatalogKey, "<catalog>")`. Unlocks
  Glue alternate catalogs and federated query sources.
- `Config.SetCatalog` / `Config.GetCatalog` and the `DefaultCatalog`
  constant (`"AwsDataCatalog"`).

### Fixed
- Mock-test `aws/transport/http.ResponseError` literal switched to
  keyed fields so the package now passes `go vet` and CI lint
  workflows no longer need `-vet=off`.
- `buildExecutionParams` now returns a `nil` slice (not an empty slice)
  when there are no query arguments, so `StartQueryExecution` leaves
  `ExecutionParameters` unset. Athena rejects non-parameterized queries
  that carry an empty `ExecutionParameters` array.
- Server-side query is now stopped when the caller's context is cancelled
  mid-`GetQueryExecution`. Previously, a context cancellation that landed
  during the in-flight status poll returned an error without issuing
  `StopQueryExecution`, leaving the Athena query running (and scanning
  bytes) until natural completion.

### Changed
- **Breaking:** logging switched from `go.uber.org/zap` to the
  standard-library `log/slog`. `LoggerKey` ctx values must now be
  `*slog.Logger` (previously `*zap.Logger`). `DriverTracer.Logger()` /
  `SetLogger` / `NewObservability` signatures changed accordingly.
  The exported `DebugLevel` / `InfoLevel` / `WarnLevel` / `ErrorLevel`
  constants are still re-exported from the driver package; they now
  alias `slog.Level` values, so call sites that did
  `obs.Log(drv.ErrorLevel, "...")` keep working. Field constructors
  in user code change from `zap.String(...)` to `slog.String(...)`
  (and friends).
- Driver no longer pulls `go.uber.org/zap` (or `zapcore`,
  `go.uber.org/multierr`) into consumers' module graph. The only
  remaining uber transitive is `go.uber.org/atomic`, required by
  `uber-go/tally/v4`.
- CI: replaced the stale `.travis.yml` (Go 1.12–1.16, `uber/athenadriver`
  paths, archived `golang.org/x/lint/golint`) with
  `.github/workflows/ci.yml`. The workflow runs `go vet`, `gofmt -s`,
  and `go test -race` across Go 1.22, 1.23, 1.24, uploads coverage to
  Codecov, and builds the `athenareader/` CLI as a separate job.
  `Makefile` simplified to match.
- README: added an "About this fork" section explaining the fork
  chain (uber → grafana → CorkCyber), why this Cork Cyber fork picks
  up from grafana's aws-sdk-go-v2 port, and what new surface area
  this repo adds (Athena API features since 2022, log/slog, OTel
  direction, aws.Config injection, hardened polling). Badge URLs and
  the "made by" badge swapped to Cork Cyber; FOSSA badge (pointing
  at uber's project ID) removed.
- `athenareader/` is now a separate Go module
  (`github.com/CorkCyber/athenadriver/athenareader`) with its own
  `go.mod`. `lib/configfx` + `lib/queryfx` moved under
  `athenareader/lib/` since they were only used by the CLI. Library
  consumers that import just the driver
  (`github.com/CorkCyber/athenadriver/go`) no longer transitively pull
  `go.uber.org/fx`, `go.uber.org/config`, `go.uber.org/dig`, BurntSushi
  TOML, `golang.org/x/lint`, or `golang.org/x/tools` into their
  module graph. The CLI's `go.mod` uses a `replace` directive against
  `../` for in-repo development; release builds should override that.
- `(*Rows).fetchNextPage` now uses
  `athena.NewGetQueryResultsPaginator` (aws-sdk-go-v2) instead of the
  hand-rolled `NextToken` loop. Behavior unchanged; pagination state lives
  on the paginator rather than `ResultOutput.NextToken`.
- Replaced the vendored `aws-sdk-go` v1 `awsutil.Prettify` (`go/prettify.go`)
  with a small, purpose-built formatter that handles
  `athena/types.WorkGroupConfiguration` (and its nested `ResultConfiguration`,
  `EncryptionConfiguration`, `EngineVersion`) directly. No external dependency
  on dead-upstream code; output of `Config.Stringify()` is unchanged for the
  fields exercised by existing tests.
- `go.mod` directive relaxed from `go 1.26.3` (Grafana plugin CI hygiene
  default) to `go 1.22` so general consumers on supported Go toolchains can
  build the driver.
- `examples/metrics.go` migrated from `cactus/go-statsd-client/statsd` v1 to
  `cactus/go-statsd-client/v5` (matches what `uber-go/tally/v4` already
  requires for its statsd reporter).

### Removed
- Self-import of `github.com/uber/athenadriver` from `go.mod`. All in-repo
  imports rewritten to `github.com/CorkCyber/athenadriver`.
- Indirect dependencies dropped by `go mod tidy` after the import rewrite:
  `aws-sdk-go` v1, `jmespath`, `uber-go/tally` v3.

## [2.0.0] — aws-sdk-go-v2 migration (`CorkCyber/athenadriver`)

First release under `github.com/CorkCyber/athenadriver`. Fork chain:
[`uber/athenadriver`](https://github.com/uber/athenadriver) (dormant
since 2025-05-31) →
[`grafana/athenadriver`](https://github.com/grafana/athenadriver)
(carried the v1 → v2 AWS SDK port to
[`aws-sdk-go-v2`](https://github.com/aws/aws-sdk-go-v2)) →
this repo, which picks up from Grafana and adds the surface area
covered by this CHANGELOG.

### Migration notes for consumers coming from `uber/athenadriver`

1. **Import path** changes from `github.com/uber/athenadriver` to
   `github.com/CorkCyber/athenadriver`. Update all imports in your code and
   `go.mod`.
2. Driver registration name is unchanged (`drv.DriverName`), so existing
   `sql.Open("awsathena", dsn)` calls keep working.
3. AWS SDK types exposed by the driver (e.g. workgroup configuration,
   result configuration, encryption configuration) now come from
   `github.com/aws/aws-sdk-go-v2/service/athena/types` instead of
   `github.com/aws/aws-sdk-go/service/athena`. Any code that constructs these
   types directly must be updated to the v2 type names and pointer
   conventions.
4. Credential plumbing: the v1 `*credentials.Credentials` chain is replaced
   by `aws.Config` / `aws.CredentialsProvider`. DSN keys (`accessID`,
   `secretAccessKey`, `sessionToken`, `region`) are unchanged.
5. Minimum Go version: `1.22`.

### Included upstream fixes (carried forward from `uber/athenadriver`)

- Pass underlying AWS error when getting workgroup (PR #64, #68, #70).
- Nil-pointer fix on workgroup tags (PR #69).
- Leave `ExecutionParameters` nil if no args, fixing queries that pre-date
  Athena's parameterized-query support (PR #74).
- Tally `v3` → `v4` upgrade (PR #76).
- README documentation for Athena parameterized queries.

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
