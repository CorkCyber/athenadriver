# Contributing

PRs and issues are welcome. Please [open an issue][open-issue] first if
you're proposing a non-trivial change, so we can agree on the approach
before code lands.

Treat fellow contributors with respect; see the
[code of conduct](CODE_OF_CONDUCT.md).

## Setup

[Fork][fork] the repo, then clone your fork:

```bash
git clone git@github.com:your_github_username/athenadriver.git
cd athenadriver
git remote add upstream https://github.com/CorkCyber/athenadriver.git
git fetch upstream
```

The repository is six Go modules:

- `./` — the driver (`./go/...`)
- `./athenareader` — CLI tool
- `./examples` — runnable example programs
- `./scope/otel`, `./scope/tally`, `./scope/statsd` — optional metrics adapters

Run the unit + race tests:

```bash
make test           # driver tests with -race
make lint           # go vet + gofmt -s
make cover          # writes cover.out and cover.html
```

To exercise the CLI or examples build:

```bash
make athenareader   # builds the CLI module
make examples       # builds every example
```

## Making changes

```bash
git checkout master
git fetch upstream
git rebase upstream/master
git checkout -b your_branch_name
```

Make your changes; keep `make test` and `make lint` clean. Push to
your fork and open a PR via GitHub.

We're much more likely to merge your PR if you:

- add tests for new behavior;
- write a [good commit message][commit-message];
- keep backwards compatibility where reasonable, and call out
  intentional breaks in the PR description and CHANGELOG.

[fork]: https://github.com/CorkCyber/athenadriver/fork
[open-issue]: https://github.com/CorkCyber/athenadriver/issues/new
[commit-message]: http://tbaggery.com/2008/04/19/a-note-about-git-commit-messages.html
