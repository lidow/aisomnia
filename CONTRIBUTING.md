# Contributing

Thanks for helping. Bug reports, questions, documentation fixes and code are all welcome.

## Before you start

- Search the [issues](https://github.com/lidow/aisomnia/issues) first.
- For a new feature or a change in behavior, open an issue to discuss it before writing code. Small fixes can go straight to a pull request.
- `aisomnia` changes system settings. Every change must stay reversible by `aisomnia reset`, must print the command before running it, and must never read or store a password.

## Development setup

- macOS 14 or later and Go 1.22 or later. `brew install go golangci-lint` covers the tooling.
- `make build` builds `./aisomnia`; `make install` puts it in `~/.local/bin`.
- `make test` runs the unit tests and `go vet`. The tests never execute `pmset`, `defaults`, `sysadminctl`, `ioreg`, `launchctl` or `caffeinate`: all process calls go through the `Runner` interface in `macos.go`, and the tests inject a fake. Keep it that way.
- `make lint` runs golangci-lint with the repository configuration. `make vuln` runs govulncheck.

## Pull requests

1. Fork, create a branch from `main`.
2. Add or update tests for what you change. A new macOS command gets a parser test and an ordering test in `aisomnia_test.go`.
3. Run `make check` (format, vet, lint, test) locally.
4. Add a line under `Unreleased` in `CHANGELOG.md`.
5. Open the pull request against `main`. The template asks what you tested on which macOS version; please fill it in, because CI cannot press a power button.

Commits use [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `test:`, `chore:`). Keep the subject under 72 characters.

## Style

- Standard `gofmt` formatting, no exceptions.
- Standard library first. The only dependency is the TOML parser; adding another needs a reason in the pull request.
- No cgo.
- Error messages tell the user what to do next (`run aisomnia apply`, `pass --force`).
- Documentation is plain language: short sentences, one idea each, no jargon the README has not explained.

## Reporting a security issue

Please do not open a public issue. See [SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the [MIT License](LICENSE).
