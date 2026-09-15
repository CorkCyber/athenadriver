# TODO

Remaining work. Completed items live in commit history + `CHANGELOG.md`.

## `database/sql` interface coverage

- [ ] `RowsColumnTypeLength(index int) (length int64, ok bool)` — for `varchar(n)` / `char(n)`. **Blocked**: SDK v2 `ColumnInfo` has no length field; Athena does not expose declared varchar length in metadata. Skip unless source surfaces it.

## Observability

Done: the driver core no longer depends on tally — metrics go through the
2-method `athenadriver.Scope` interface via `MetricsKey`, with separate
`scope/otel`, `scope/tally`, and `scope/statsd` adapter modules (see
CHANGELOG.md's v2.0.0 section, "Metrics decoupled from tally").

- [ ] OTel tracing spans around `StartQueryExecution`, the `GetQueryExecution`
      polling loop, `GetQueryResults` pagination, and `convertRow`. Attributes:
      query ID, workgroup, catalog, scan bytes, engine version, statement
      type. Unlocks Sentry traces, Datadog APM, Honeycomb, Tempo, etc. via
      standard OTel exporters. (Metrics via `Scope` already ship; this is
      tracing specifically, still unstarted.)
- [ ] Structured query metrics — query ID, scan bytes, engine version, execution time. Already partly in `cost.go`; expose via `Rows` accessor or context callback.

## Bulk read performance

- [ ] Optional CSV-from-S3 fast path for large result sets — read `${OutputLocation}/${QueryID}.csv` directly from S3 instead of paginating `GetQueryResults` (max 1000 rows/page). Athena-jdbc-driver and PyAthena both do this. Order-of-magnitude speedup for big reads.
- [ ] Streaming `Rows.Next` — currently buffers a full page. With CSV path can stream row-by-row.

## Release / project hygiene

- [ ] Cut a tagged release on `CorkCyber/athenadriver` (e.g. `v2.0.0`). No tags currently pushed; consumers stuck pinning SHAs. Semver communicates SDK-v2 break.

## Lower priority / nice-to-have

- [ ] Athena Spark / notebooks support (`ExecutionRole`, session calculation APIs). Niche, skip unless a user asks.
- [ ] Async query mode — `StartQueryExecution` only, return QID, caller polls separately. Pseudo-command `PCGetQID` already does half of this; formalize.

## Out of scope

- Rewriting `xwb1989/sqlparser` usage — already dropped; the driver no longer reformats user-supplied SQL.
- Rewriting `athenareader/` CLI — separate tool, not the driver.
