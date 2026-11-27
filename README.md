# aisomnia

**Keep your Mac awake for the work, locked for compliance, asleep when the lid closes.**

`aisomnia` is a small macOS command-line tool for people who run long jobs on a laptop: AI coding agents, builds, test suites, syncs, remote sessions. It declares your power, lock and lid behavior in one file, applies it with one command, and tells you when the Mac has drifted from it.

```
$ aisomnia status
profile: aisomnia default
SETTING                               DECLARED   LIVE       STATE   NOTE
pmset battery sleep                   0          0          OK
pmset battery displaysleep            5          5          OK
pmset ac sleep                        0          0          OK
pmset ac displaysleep                 30         30         OK
power button: Sleep On Power Button   0          0          OK
screen saver idle (s)                 300        300        OK
screen lock delay                     immediate  immediate  OK
lid watcher (launchd agent)           running    running    OK
```

## What it does

| Rule | Why |
|---|---|
| **Never sleep on idle**, on battery or on power | An agent that is halfway through a refactor, a container build, or an `rsync` must not be frozen because you walked away. |
| **Lock the screen after 5 minutes** of inactivity, with authentication to unlock | Most security policies require an unattended screen to lock within a few minutes. Work keeps running behind the lock. |
| **Display off** after 5 minutes on battery, 30 on power | Saves the battery and the panel without stopping anything. |
| **Sleep only when the lid closes**, also with an external display attached | The lid is the one gesture that means "I am done". A small watcher sleeps the Mac in clamshell mode too, which macOS alone does not do. |
| **Power button locks, does not sleep** | A short press turns the display off and locks instead of killing your sessions. |
| **Network stays up** while locked | TCP keep-alive on both power sources, wake-on-LAN on power: SSH into your locked Mac works. |
| **Everything is reversible** | `aisomnia reset` restores the macOS baseline and removes the watcher. |

The tool only drives what macOS already offers: `pmset`, `defaults`, `sysadminctl`, `caffeinate`, `ioreg` and `launchd`. Nothing is stored except a small log and the hold files. No network calls, no telemetry, no root daemon.

## Install

Homebrew tap (macOS 14 or later, Apple silicon and Intel):

```sh
brew install lidow/tap/aisomnia
```

From source (Go 1.22 or later):

```sh
go install github.com/lidow/aisomnia@latest
```

Or clone the repository and run `make install`, which builds into `~/.local/bin`.

## Quick start

```sh
aisomnia status            # what the profile declares, what the Mac has, what drifts
aisomnia apply --dry-run   # the exact commands apply would run
aisomnia apply             # asks for sudo once, prints each command before running it
```

After the first `apply`, macOS asks you to restart once (for the power-button setting) and to log out and in once (for the login-window setting). `status` says so until you do.

To customize, write an editable copy of the profile and change what you like:

```sh
aisomnia profile --init    # writes ~/.config/aisomnia/profile.toml
$EDITOR ~/.config/aisomnia/profile.toml
aisomnia apply
```

The profile reference is in [docs/profile.md](docs/profile.md).

## Commands

| Command | What it does |
|---|---|
| `aisomnia status [--json]` | Table of every managed setting. Exit code 1 while anything drifts. |
| `aisomnia apply [--dry-run]` | Applies only the settings that drift. One sudo prompt. Prints each command first. |
| `aisomnia reset [--dry-run]` | Restores the macOS baseline and stops the lid watcher. |
| `aisomnia lock`, `aisomnia display-off` | Display off and immediate lock. The Mac stays awake. Works from Spotlight, Alfred or a Shortcut. |
| `aisomnia sleep` | Sleep now. |
| `aisomnia hibernate` | One deep sleep (`hibernatemode 25`) with the normal mode restored after wake. |
| `aisomnia wake-at "2026-01-15 07:30"`, `aisomnia wake-in 2h` | Schedule a wake. |
| `aisomnia hold display\|lock\|lid\|standby <duration>` | Hold one rule off for a while. See [Holds](#holds). |
| `aisomnia holds`, `aisomnia release` | List active holds, end them all. |
| `aisomnia lidwatch start\|stop\|status\|run` | Manage the lid watcher. |
| `aisomnia profile [--init\|--path]` | Print the active profile, write an editable copy, show which file is in use. |

Exit codes: `0` ok or no drift, `1` drift or a failed command, `2` usage error.

## Holds

A hold is a one-time exception with a timer. The profile does not change. The hold ends when the timer runs out, when you press Ctrl-C, or when the terminal that runs it closes.

| You want | Run | Notes |
|---|---|---|
| Keep the display on during a talk or a long build | `aisomnia hold display 2h` | The screen still locks after 5 minutes of no input unless you also hold the lock. |
| No lock while you present or sit on a call | `aisomnia hold lock 1h` | Use it only while you are at the Mac; it suspends the compliance rule. |
| Carry the Mac closed and running across the building | `aisomnia hold lid 30m` | Needs sudo once. Ends on its own at 20 % battery. A closed laptop in a bag gets warm. |
| Fast wake tomorrow morning, no deep sleep tonight | `aisomnia hold standby 8h` | Needs sudo once. |

Display and lock holds use `caffeinate` assertions. Lid and standby holds change one `pmset` value through a root helper that always reverts, on the timer, on a signal, or at the battery floor. If the Mac crashes during a lid hold, the next `aisomnia status` reports the leftover `disablesleep` and `aisomnia apply` reverts it.

## The lid watcher

macOS keeps a laptop awake when the lid is closed with an external display and power attached. If that is not what you want, the watcher closes the gap: a launchd user agent polls the clamshell sensor every 2 seconds and runs `pmset sleepnow` when the lid goes from open to closed, or when the Mac wakes with the lid still closed (an external keyboard woke it). It never acts on its first observation, so logging in with the lid already closed is left alone, and it pauses during a lid hold.

It uses about 7 MB of memory and no measurable CPU. `aisomnia lidwatch status` shows its state and the last log line; the log is at `~/Library/Logs/aisomnia/aisomnia.log`, rotated at 1 MB. Set `enabled = false` under `[lidwatch]` in your profile if you prefer macOS's clamshell behavior.

## Battery

With idle sleep off, a Mac left open and locked keeps running until the battery is empty. Close the lid when you put it away. `aisomnia status` prints a reminder whenever the Mac is on battery.

## States and transitions

The five states (active, display off, locked, sleep, standby), what triggers each, how to force it and how to hold it off are described in [docs/states.md](docs/states.md).

## Compliance notes

The default profile locks after 5 minutes of inactivity with authentication to unlock, which satisfies the common "clean screen" requirement of 5 minutes or less. A longer delay is one line in the profile, but check your policy first. `aisomnia status --json` is suitable for an audit script: `drift: false` means the Mac matches the declared profile.

## Uninstall

```sh
aisomnia reset         # macOS baseline, watcher removed
brew uninstall aisomnia
rm -rf ~/.config/aisomnia ~/Library/Logs/aisomnia "~/Library/Application Support/aisomnia"
```

## Development

```sh
make test     # go test, go vet
make lint     # golangci-lint
make vuln     # govulncheck
make build    # ./aisomnia
```

Unit tests never touch macOS: every command runs through one `Runner` interface and the tests inject a fake. See [CONTRIBUTING.md](CONTRIBUTING.md). Releases are cut by tagging (`vX.Y.Z`); GitHub Actions builds the binaries, publishes the release and updates the Homebrew formula. See [docs/releasing.md](docs/releasing.md).

## License

[MIT](LICENSE) © 2026 DevJestic.
