# Security policy

`aisomnia` runs commands with `sudo` on your Mac and changes power and lock settings. Bugs here can leave a machine unlocked or asleep when it should not be. Reports are welcome and taken seriously.

## Supported versions

The latest release on the `main` branch. Older tags receive no fixes.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting: open the repository's **Security** tab and choose **Report a vulnerability**. Do not open a public issue for anything that could leave a Mac unlocked, grant unexpected privileges, or run a command the user did not see.

Include the macOS version, the `aisomnia` version (`aisomnia version`), the profile in use (`aisomnia profile`), and the exact steps. You will get an acknowledgement within 7 days and a fix or a workaround as soon as one exists; the advisory is published with the release that fixes it.

## What the tool does with privileges

- `sudo` is requested once per `apply`, `reset`, `wake-at`, `wake-in`, `hibernate` and the lid or standby holds, through the terminal; the tool never stores or forwards the password.
- The screen-lock delay is set by `sysadminctl -screenLock`, which asks for the login password in the terminal. The password does not pass through `aisomnia`.
- The lid watcher runs as a launchd **user** agent without privileges. The only root helper is `aisomnia _hold-run`, started by `aisomnia hold lid|standby` through `sudo`; it sets one `pmset` value and reverts it.
- No network access, no telemetry, no files outside `~/.config/aisomnia`, `~/Library/Logs/aisomnia`, `~/Library/Application Support/aisomnia`, `~/Library/LaunchAgents` and the macOS preference domains the profile manages.

## Automated checks

Every pull request runs `go vet`, golangci-lint, `govulncheck` and CodeQL. Dependabot opens pull requests for Go modules and GitHub Actions weekly. OpenSSF Scorecard runs weekly on `main`.
