// aisomnia keeps a Mac awake for long-running work (AI agents, builds, syncs), locked for
// compliance, and asleep when the lid closes. It applies a declared power profile and reports drift.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	appName    = "aisomnia"
	agentLabel = "dev.aisomnia.lidwatch"
)

// version is set by the release build (-ldflags "-X main.version=v1.2.3").
var version = "dev"

const usage = `aisomnia — keep your Mac awake for the work, locked for compliance, asleep when the lid closes

Usage:
  aisomnia status [--json]            managed settings: declared / live / OK or DRIFT (exit 1 on drift)
  aisomnia apply [--dry-run]          change what drifts; one sudo prompt; prints every command first
  aisomnia reset [--dry-run]          back to the macOS baseline; stops the lid watcher
  aisomnia lock | display-off         display off + immediate lock; the Mac stays awake
  aisomnia sleep                      sleep now
  aisomnia hibernate                  one deep sleep (hibernatemode 25), restored on wake
  aisomnia wake-at "YYYY-MM-DD HH:MM" schedule a wake
  aisomnia wake-in <duration>         wake after 2h, 90m, 1h30m
  aisomnia hold display|lock|lid|standby <duration>
                                     hold a state off once; Ctrl-C or closing the terminal ends it
  aisomnia holds | release            list or end active holds
  aisomnia lidwatch start|stop|status|run
  aisomnia profile [--init|--path]    print the active profile; --init writes an editable copy
  aisomnia version

Options (before the verb):
  --profile PATH   use this profile (or AISOMNIA_PROFILE); default ~/.config/aisomnia/profile.toml
                   when it exists, else the built-in profile

Exit codes: 0 ok / no drift · 1 drift or a command failed · 2 usage
`

// exitError carries the process exit code out of a verb.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return e.msg }

func exitf(code int, format string, a ...any) error {
	return &exitError{code: code, msg: fmt.Sprintf(format, a...)}
}

type app struct {
	runner      Runner
	profile     *Profile
	profilePath string // "" when the built-in profile is active
	defaults    *Profile
	home        string
	stdout      io.Writer
	stderr      io.Writer
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var profileFlag string
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch {
		case args[0] == "--profile" && len(args) > 1:
			profileFlag, args = args[1], args[2:]
		case strings.HasPrefix(args[0], "--profile="):
			profileFlag, args = strings.TrimPrefix(args[0], "--profile="), args[1:]
		case args[0] == "--help" || args[0] == "-h":
			fmt.Fprint(stdout, usage)
			return 0
		case args[0] == "--version":
			fmt.Fprintf(stdout, "%s %s\n", appName, version)
			return 0
		default:
			fmt.Fprintf(stderr, "%s: unknown option %s\n%s", appName, args[0], usage)
			return 2
		}
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "version":
		fmt.Fprintf(stdout, "%s %s\n", appName, version)
		return 0
	case "help":
		fmt.Fprint(stdout, usage)
		return 0
	}

	home, _ := os.UserHomeDir()
	path := resolveProfilePath(profileFlag, os.Getenv("AISOMNIA_PROFILE"), home)
	profile, err := loadProfile(path)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return 2
	}
	defaults, err := parseProfile(defaultsTOML)
	if err != nil {
		fmt.Fprintf(stderr, "%s: defaults profile: %v\n", appName, err)
		return 2
	}
	a := &app{runner: execRunner{}, profile: profile, profilePath: path, defaults: defaults, home: home, stdout: stdout, stderr: stderr}

	err = a.dispatch(verb, rest)
	var ee *exitError
	if errors.As(err, &ee) {
		if ee.msg != "" {
			fmt.Fprintf(stderr, "%s: %s\n", appName, ee.msg)
		}
		return ee.code
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", appName, err)
		return 1
	}
	return 0
}

func (a *app) dispatch(verb string, args []string) error {
	switch verb {
	case "status":
		return a.cmdStatus(args)
	case "apply":
		return a.cmdApply(args, a.profile, "apply")
	case "reset":
		return a.cmdApply(args, a.defaults, "reset")
	case "lock", "display-off", "sleep", "hibernate", "wake-at", "wake-in":
		return a.cmdForce(verb, args)
	case "hold":
		return a.cmdHold(args)
	case "holds":
		return a.cmdHolds()
	case "release":
		return a.cmdRelease()
	case "_hold-run":
		return a.cmdHoldRun(args)
	case "lidwatch":
		return a.cmdLidwatch(args)
	case "profile":
		return a.cmdProfile(args)
	default:
		return exitf(2, "unknown verb %q\n%s", verb, usage)
	}
}
