# athenadriver examples

Sample code for `github.com/CorkCyber/athenadriver/v2/go`. Each example
lives in its own subdirectory and builds as a standalone `main`.

This directory is its own Go module
(`github.com/CorkCyber/athenadriver/examples`) so the driver itself
does not carry the examples' dependencies. The module uses a `replace`
directive against `../` for in-repo development; release builds should
drop or override the replace.

## Layout

| Path | What it shows |
| --- | --- |
| `auth/` | The supported AWS authentication methods. |
| `lambda/Go/` | Running Athena queries from an AWS Lambda function. |
| `logging/` | Wiring an `slog.Logger` via context. |
| `metrics/` | Wiring an OpenTelemetry meter via the `scope/otel` adapter. |
| `maskcolumn/` | Masking columns with substitute values. |
| `pc_get_driver_version/`, `pc_get_query_id/`, `pc_get_query_id_status/`, `pc_stop_query_id/` | Each pseudo-command (`pc:get_driver_version`, etc.). |
| `ping/` | Health check / `db.Ping`. |
| `qid/` | Querying directly with a previously captured Athena query ID. |
| `querycancel/` | Cancelling queries via context. |
| `readonly/` | Read-only mode on the driver. |
| `reconnect/` | Automatic reconnection. |
| `trans/` | Demonstrating that Athena does not support transactions. |
| `types/` | The Athena data types the driver surfaces. |
| `workgroup_with_tag/` | Workgroup creation and tagging. |
| `perf/benchmark/`, `perf/concurrency/` | Stress / concurrency tests. |
| `query/...` | One subdirectory per supported SQL statement shape. |

## Running

```bash
cd examples
go run ./query/dml_select_simple
```

The examples expect AWS credentials and an S3 results bucket. Fill in
the constants at the top of each `main.go` or rely on the AWS SDK
default credential chain (env vars, `~/.aws/config`, IMDS, etc.).
