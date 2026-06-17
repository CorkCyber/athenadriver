# TODO

Remaining work. Completed items live in commit history + `CHANGELOG.md`.

## `database/sql` interface coverage

- [ ] `RowsColumnTypeLength(index int) (length int64, ok bool)` — for `varchar(n)` / `char(n)`. **Blocked**: SDK v2 `ColumnInfo` has no length field; Athena does not expose declared varchar length in metadata. Skip unless source surfaces it.

## Observability — move to OpenTelemetry

Driver currently instruments via `uber-go/tally`. Tally is in maintenance mode (last meaningful release 2022) and has no tracing surface. OTel is the right destination: also the only viable path to land Athena query telemetry in Sentry, whose standalone metrics product was deprecated Oct 2024.

- [ ] **Phase 1 — additive OTel support.** Driver accepts an optional `metric.Meter` + `trace.Tracer` on `Config` (or via context, mirroring `MetricsKey`). When set, driver fires both tally counters AND OTel instruments. No breaking change for existing tally consumers.
- [ ] **Phase 2 — OTel tracing spans** around `StartQueryExecution`, `GetQueryExecution` polling loop, `GetQueryResults` pagination, `convertRow`. Attributes: query ID, workgroup, catalog, scan bytes, engine version, statement type. Unlocks Sentry traces, Datadog APM, Honeycomb, Tempo, etc. via standard OTel exporters.
- [ ] **Phase 3 — deprecate tally**. Mark `MetricsKey` deprecated. Emit deprecation log when tally scope is supplied without an OTel meter. Cut tally entirely in v3.0.
- [ ] **Phase 4 — drop statsd example**. Replace `examples/metrics/main.go` with `examples/otel/main.go` showing OTel meter + OTLP exporter wiring.
- [ ] Structured query metrics — query ID, scan bytes, engine version, execution time. Already partly in `cost.go`; expose via `Rows` accessor or context callback. Wire to OTel attributes in Phase 2.

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
