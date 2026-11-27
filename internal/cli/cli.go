// Package cli is the aisomnia command line: it parses arguments, resolves the profile and the
// paths under the user's home, and wires the internal packages together.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lidow/aisomnia/internal/engine"
	"github.com/lidow/aisomnia/internal/hold"
	"github.com/lidow/aisomnia/internal/lidwatch"
	"github.com/lidow/aisomnia/internal/macos"
	"github.com/lidow/aisomnia/internal/power"
	"github.com/lidow/aisomnia/internal/profile"
)

const name = "aisomnia"

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

// app holds what every verb needs.
type app struct {
	runner      macos.Runner
	profile     *profile.Profile
	profilePath string // "" when the built-in profile is active
	baseline    *profile.Profile
	home        string
	uid         string
	stdout      io.Writer
	stderr      io.Writer
}

func (a *app) stateDir() string  { return filepath.Join(a.home, "Library", "Application Support", name) }
func (a *app) holds() hold.Store { return hold.Store{Dir: filepath.Join(a.stateDir(), "holds")} }
func (a *app) agent() lidwatch.Agent {
	return lidwatch.Agent{
		Runner:    a.runner,
		UID:       a.uid,
		PlistPath: filepath.Join(a.home, "Library", "LaunchAgents", lidwatch.Label+".plist"),
		LogDir:    filepath.Join(a.home, "Library", "Logs", name),
	}
}

// watcher adapts the agent to engine.Watcher with this binary and the active profile.
type watcher struct{ a *app }

func (w watcher) Start() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return w.a.agent().Start(exe, w.a.profilePath)
}

func (w watcher) Stop() error { return w.a.agent().Stop() }

// Run executes the command line and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	return run(args, stdout, stderr, version, macos.Exec{})
}

func run(args []string, stdout, stderr io.Writer, version string, runner macos.Runner) int {
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
			fmt.Fprintf(stdout, "%s %s\n", name, version)
			return 0
		default:
			fmt.Fprintf(stderr, "%s: unknown option %s\n%s", name, args[0], usage)
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
		fmt.Fprintf(stdout, "%s %s\n", name, version)
		return 0
	case "help":
		fmt.Fprint(stdout, usage)
		return 0
	}

	home, _ := os.UserHomeDir()
	path := profile.ResolvePath(profileFlag, os.Getenv("AISOMNIA_PROFILE"), home)
	p, err := profile.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 2
	}
	base, err := profile.Baseline()
	if err != nil {
		fmt.Fprintf(stderr, "%s: baseline profile: %v\n", name, err)
		return 2
	}
	a := &app{runner: runner, profile: p, profilePath: path, baseline: base, home: home, uid: macos.CurrentUID(), stdout: stdout, stderr: stderr}

	err = a.dispatch(verb, rest)
	var ee *exitError
	switch {
	case errors.As(err, &ee):
		if ee.msg != "" {
			fmt.Fprintf(stderr, "%s: %s\n", name, ee.msg)
		}
		return ee.code
	case err != nil:
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
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
		return a.cmdApply(args, a.baseline, "reset")
	case "lock", "display-off", "sleep", "hibernate", "wake-at", "wake-in":
		return a.cmdForce(verb, args)
	case "hold":
		return a.cmdHold(args)
	case "holds":
		return a.cmdHolds()
	case "release":
		a.holds().Release(a.stdout)
		return nil
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

// parseDuration accepts Go durations (2h, 90m, 1h30m, 45s) and a bare number as minutes.
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("duration must be positive")
		}
		return time.Duration(n) * time.Minute, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("duration %q: use 2h, 90m, 1h30m or minutes", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return d, nil
}

// sudoErr maps a declined sudo prompt to exit code 1 with its message.
func sudoErr(err error) error {
	if errors.Is(err, engine.ErrSudoDeclined) || errors.Is(err, power.ErrSudoDeclined) {
		return exitf(1, "%v", err)
	}
	return err
}
