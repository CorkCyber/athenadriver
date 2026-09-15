# TODO

Remaining work. Completed items live in commit history + `CHANGELOG.md`.

## `database/sql` interface coverage

- [ ] `RowsColumnTypeLength(index int) (length int64, ok bool)` — for `varchar(n)` / `char(n)`. **Blocked**: SDK v2 `ColumnInfo` has no length field; Athena does not expose declared varchar length in metadata. Skip unless source surfaces it.

## Observability

Done: the driver core no longer depends on tally — metrics go through the
2-method `athenadriver.Scope` interface via `MetricsKey`, with separate
`scope/otel`, `scope/tally`, and `scope/statsd` adapter modules (see
CHANGELOG.md's v2.0.0 section, "Metrics decoupled from tally").

Done: tracing. One span per `QueryContext`/`ExecContext` call via the new
`athenadriver.Tracer`/`Span` interfaces + `TracerKey` context value,
mirroring the `Scope` design — `scope/otel.NewTracer(otel.Tracer)` bridges
it to OpenTelemetry. Spans are tagged CLIENT-kind with the OTel database
semantic-convention attributes (`db.system.name=aws.athena`, `db.namespace`,
`db.operation.name`) plus `athena.query_id`/`workgroup`/`catalog`/
`statement_type`/`data_scanned_bytes`, so a backend that understands those
conventions (Sentry, Datadog APM, Honeycomb, Tempo, ...) renders it as a DB
span. `db.query.text` is deliberately omitted — the DDL-interpolation path
embeds literal argument values in the query text, and a trace should not
leak those by default.

- [ ] Finer-grained child spans inside one query's span — `StartQueryExecution`,
      the poll loop (as one span, not one per poll iteration), and the first
      `GetQueryResults` page fetch — for backends that want to see where time
      went inside a single query. NOT a span per row/cell in `convertRow`;
      that would be pathological on a large result set. Nice-to-have, not
      required for a backend to recognize and render the top-level span.
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
