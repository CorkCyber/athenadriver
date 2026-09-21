# Changelog

## 1.0.0 (2026-09-21)


### ⚠ BREAKING CHANGES

* go 1.26 is now the minimum supported Go version.
* **athenareader:** configfx.Params/Result and queryfx's fx wrapper types are gone. configfx.New()/queryfx.New() are now plain functions.
* fix stale/incorrect documentation across README, CHANGELOG, examples
* go 1.24 is now the declared minimum across every module.
* **athenareader:** athenareader no longer fetches a fallback config from GOPATH or a remote URL. Only $HOME and the working directory are checked before the embedded default.
* a tally.Scope injected via MetricsKey no longer type-asserts. Bridge through scope/otel, scope/tally, scope/statsd, or a small custom adapter. Import path changes to github.com/CorkCyber/athenadriver/v2/go.
* Config is a typed struct now. Old GetX/SetX accessors for non-validated fields are gone; use direct field access. See CHANGELOG.md's v2.0.0 migration guide.

### Features

* decouple metrics from tally, adopt /v2 module path ([a662404](https://github.com/CorkCyber/athenadriver/commit/a662404d579618cefc4b53897ef31fa6ee53eb15))
* initial v2 rewrite scaffolding ([ed36067](https://github.com/CorkCyber/athenadriver/commit/ed3606771dc3062e38a9d786c98a3c5ed7abe98f))
* rewrite Config as a typed struct (v2.0.0) ([d50dcf8](https://github.com/CorkCyber/athenadriver/commit/d50dcf8745df25af186fdcb98cbbe3d87586e165))


### Bug Fixes

* **athenareader:** add the missing replace directive ([9ca90f5](https://github.com/CorkCyber/athenadriver/commit/9ca90f546809007b37ee546f0e80844218893796))
* **athenareader:** propagate stream errors, honor config fastfail, use os.UserHomeDir ([3693a62](https://github.com/CorkCyber/athenadriver/commit/3693a621325f95219c9c4c07ae9f8732d868c0d8))
* **athenareader:** stop fetching default config from a third party ([2d9821e](https://github.com/CorkCyber/athenadriver/commit/2d9821e98fccf1b236c17370a4ead1e4a44322f9))


### Documentation

* fix stale/incorrect documentation across README, CHANGELOG, examples ([4ea2bc9](https://github.com/CorkCyber/athenadriver/commit/4ea2bc9b6ef03ae44463789d36737c9a65122e74))


### Miscellaneous Chores

* bump AWS SDK v2 + go.uber.org/config across all modules ([fd76f01](https://github.com/CorkCyber/athenadriver/commit/fd76f0143b53052d5f4608fc8a6f616d54b40a29))
* bump go floor to 1.26, modernize accordingly ([a21f988](https://github.com/CorkCyber/athenadriver/commit/a21f9886953d2cc400067e9ebab0125b54bc8384))


### Code Refactoring

* **athenareader:** drop go.uber.org/fx + go.uber.org/config ([cd6e959](https://github.com/CorkCyber/athenadriver/commit/cd6e959c8f7cf0cdaf9e4bbccf129c10e98f94ce))
