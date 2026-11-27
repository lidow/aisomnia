# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-11-27

### Added

- Declared power profile (`pmset` per power source, power-button keys, screen saver and lock delay, lid watcher) with a built-in default and an editable user copy (`aisomnia profile --init`).
- `status` with a drift table and `--json`, `apply` and `reset` with `--dry-run`, one sudo prompt, every command printed before it runs.
- Force verbs: `lock`, `display-off`, `sleep`, `hibernate`, `wake-at`, `wake-in`.
- One-time holds with timers: `hold display|lock|lid|standby`, `holds`, `release`; lid holds end at a battery floor.
- Lid watcher as a launchd user agent that sleeps the Mac on lid close, also in clamshell mode.
- Unit tests with a fake command runner; CI with `go vet`, golangci-lint, govulncheck and CodeQL; Dependabot; GoReleaser with a Homebrew tap formula.

[Unreleased]: https://github.com/lidow/aisomnia/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/lidow/aisomnia/releases/tag/v0.1.0
