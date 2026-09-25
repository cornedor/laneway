# Contributing

Bug reports, ideas and pull requests are welcome.

## Before you start

- Open an issue first for anything bigger than a small fix, so we can agree
  on the approach.
- [ROADMAP.md](ROADMAP.md) lists what is planned.

## Develop

Go (the version in `go.mod`) is all you need.

```sh
make          # build ./laneway
make test     # go test ./...
go vet ./...
gofmt -l .    # must print nothing
```

Tests never talk to a real Jira: use `httptest` servers for the client and
the model harness in `internal/ui/harness_test.go` for the UI.

## Pull requests

- One change per PR, with a test that fails without it.
- Match the surrounding code: comment density, naming, idiom.
- Keep the diff small; no drive-by reformatting.
- CI (build, vet, test, gofmt, govulncheck) must be green.

By contributing you agree your work is released under the [MIT license](LICENSE).
