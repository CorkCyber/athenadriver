# Changelog

## 0.1.0 (2026-09-21)


### ⚠ BREAKING CHANGES

* go 1.26 is now the minimum supported Go version.
* go 1.24 is now the declared minimum across every module.
* a tally.Scope injected via MetricsKey no longer type-asserts. Bridge through scope/otel, scope/tally, scope/statsd, or a small custom adapter. Import path changes to github.com/CorkCyber/athenadriver/v2/go.

### Features

* add OTel-compatible tracing spans (Tracer/Span, TracerKey) ([efc4dcc](https://github.com/CorkCyber/athenadriver/commit/efc4dcc960f24f69a61e333eafa666b330ad23ac))
* decouple metrics from tally, adopt /v2 module path ([a662404](https://github.com/CorkCyber/athenadriver/commit/a662404d579618cefc4b53897ef31fa6ee53eb15))


### Performance Improvements

* **scope/otel:** cache the wrapped Counter/Timer, use RWMutex ([11264de](https://github.com/CorkCyber/athenadriver/commit/11264de28ca73140d279f1f470f7a3a26a86d38d))


### Miscellaneous Chores

* bump AWS SDK v2 + go.uber.org/config across all modules ([fd76f01](https://github.com/CorkCyber/athenadriver/commit/fd76f0143b53052d5f4608fc8a6f616d54b40a29))
* bump go floor to 1.26, modernize accordingly ([a21f988](https://github.com/CorkCyber/athenadriver/commit/a21f9886953d2cc400067e9ebab0125b54bc8384))
