package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/lidow/aisomnia/internal/engine"
	"github.com/lidow/aisomnia/internal/hold"
	"github.com/lidow/aisomnia/internal/lidwatch"
	"github.com/lidow/aisomnia/internal/macos"
	"github.com/lidow/aisomnia/internal/power"
	"github.com/lidow/aisomnia/internal/profile"
)

type statusReport struct {
	Profile  string       `json:"profile"`
	Rows     []engine.Row `json:"settings"`
	Drift    bool         `json:"drift"`
	Warnings []string     `json:"warnings"`
}

func (a *app) cmdStatus(args []string) error {
	asJSON := false
	for _, arg := range args {
		if arg != "--json" {
			return exitf(2, "status: unknown option %s", arg)
		}
		asJSON = true
	}
	live, err := macos.ReadLive(a.runner, a.uid, lidwatch.Label)
	if err != nil {
		return err
	}
	active, expired := a.holds().Clean(time.Now(), hold.ProcessAlive)
	rows, _ := engine.Assess(a.profile, live, hold.Has(active, hold.Lid), watcher{a})
	rep := statusReport{Profile: a.profile.Name, Rows: rows, Drift: engine.HasDrift(rows), Warnings: []string{}}
	if w := engine.BatteryWarning(a.profile, live); w != "" {
		rep.Warnings = append(rep.Warnings, w)
	}
	for _, h := range expired {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("expired %s hold from %s removed", h.Kind, h.Started.Format("2006-01-02 15:04")))
	}
	if asJSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Fprintln(a.stdout, string(b))
	} else {
		fmt.Fprintf(a.stdout, "profile: %s\n", rep.Profile)
		tw := tabwriter.NewWriter(a.stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "SETTING\tDECLARED\tLIVE\tSTATE\tNOTE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Setting, r.Declared, r.Live, strings.ToUpper(r.State), r.Note)
		}
		_ = tw.Flush()
		for _, h := range active {
			fmt.Fprintf(a.stdout, "hold: %s until %s (pid %d)\n", h.Kind, h.Expires.Format("15:04:05"), h.PID)
		}
		for _, w := range rep.Warnings {
			fmt.Fprintf(a.stdout, "warning: %s\n", w)
		}
	}
	if rep.Drift {
		return exitf(1, "")
	}
	return nil
}

// cmdApply runs `apply` (the profile) or `reset` (the macOS baseline).
func (a *app) cmdApply(args []string, p *profile.Profile, verb string) error {
	dryRun := false
	for _, arg := range args {
		if arg != "--dry-run" && arg != "-n" {
			return exitf(2, "%s: unknown option %s", verb, arg)
		}
		dryRun = true
	}
	live, err := macos.ReadLive(a.runner, a.uid, lidwatch.Label)
	if err != nil {
		return err
	}
	active, _ := a.holds().Clean(time.Now(), hold.ProcessAlive)
	lidHold := hold.Has(active, hold.Lid)
	if verb == "reset" && lidHold {
		return exitf(1, "a lid hold is active; run `aisomnia release` first")
	}
	rows, cmds := engine.Assess(p, live, lidHold, watcher{a})
	if len(cmds) == 0 {
		fmt.Fprintf(a.stdout, "%s: nothing to do, %d settings match %q\n", verb, len(rows), p.Name)
		return nil
	}
	if verb == "apply" {
		for _, c := range cmds {
			if strings.HasSuffix(c.Setting, "hibernatemode") {
				power.ClearMarker(a.stateDir())
			}
		}
	}
	fmt.Fprintf(a.stdout, "%s: %d command(s) for profile %q\n", verb, len(cmds), p.Name)
	err = engine.Execute(a.runner, cmds, dryRun, a.stdout)
	var se *engine.StepError
	if errors.As(err, &se) {
		return exitf(1, "%v", se)
	}
	return sudoErr(err)
}

func (a *app) cmdForce(verb string, args []string) error {
	want := map[string]int{"lock": 0, "display-off": 0, "sleep": 0, "hibernate": 0, "wake-at": 1, "wake-in": 1}[verb]
	if len(args) != want {
		switch verb {
		case "wake-at":
			return exitf(2, `usage: aisomnia wake-at "YYYY-MM-DD HH:MM"`)
		case "wake-in":
			return exitf(2, "usage: aisomnia wake-in <duration>, e.g. 2h or 90m")
		}
		return exitf(2, "%s takes no arguments", verb)
	}
	switch verb {
	case "lock", "display-off":
		return power.Lock(a.runner)
	case "sleep":
		return power.Sleep(a.runner)
	case "hibernate":
		return sudoErr(power.Hibernate(a.runner, a.profile.Pmset.AC["hibernatemode"], a.stateDir(), a.stdout))
	case "wake-at":
		t, err := time.ParseInLocation("2006-01-02 15:04", args[0], time.Local)
		if err != nil {
			return exitf(2, "wake-at: %v", err)
		}
		if err := power.WakeAt(a.runner, t, time.Now()); err != nil {
			if errors.Is(err, power.ErrSudoDeclined) {
				return sudoErr(err)
			}
			return exitf(2, "wake-at: %v", err)
		}
		return nil
	default: // wake-in
		d, err := parseDuration(args[0])
		if err != nil {
			return exitf(2, "wake-in: %v", err)
		}
		return sudoErr(power.WakeIn(a.runner, d))
	}
}

func (a *app) cmdHold(args []string) error {
	if len(args) != 2 {
		return exitf(2, "usage: aisomnia hold display|lock|lid|standby <duration>")
	}
	kind := args[0]
	d, err := parseDuration(args[1])
	if err != nil {
		return exitf(2, "hold: %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	plan, err := hold.PlanFor(kind, d, exe, a.profile.Lidwatch.BatteryFloorPercent)
	if err != nil {
		return exitf(2, "hold: %v", err)
	}
	store := a.holds()
	if kind == hold.Lid || kind == hold.Standby {
		if active, _ := store.Clean(time.Now(), hold.ProcessAlive); hold.Has(active, kind) {
			return exitf(1, "a %s hold is already active; `aisomnia holds` lists it", kind)
		}
	}
	return store.Hold(kind, d, plan, a.stdout)
}

func (a *app) cmdHoldRun(args []string) error {
	if os.Geteuid() != 0 {
		return exitf(2, "_hold-run is started by `aisomnia hold` through sudo")
	}
	if len(args) != 3 {
		return exitf(2, "usage: _hold-run lid|standby <seconds> <battery floor>")
	}
	secs, err1 := strconv.Atoi(args[1])
	floor, err2 := strconv.Atoi(args[2])
	if err1 != nil || err2 != nil {
		return exitf(2, "_hold-run: seconds and battery floor must be numbers")
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	return hold.RootHelper(a.runner, args[0], secs, floor, a.profile.Pmset.AC["standby"], a.stdout, stop)
}

func (a *app) cmdHolds() error {
	active, removed := a.holds().Clean(time.Now(), hold.ProcessAlive)
	for _, h := range removed {
		fmt.Fprintf(a.stdout, "removed stale %s hold from %s\n", h.Kind, h.Started.Format("2006-01-02 15:04"))
	}
	if len(active) == 0 {
		fmt.Fprintln(a.stdout, "no active holds")
		return nil
	}
	for _, h := range active {
		fmt.Fprintf(a.stdout, "%-8s until %s (%s left, pid %d)\n", h.Kind, h.Expires.Format("2006-01-02 15:04:05"), time.Until(h.Expires).Round(time.Second), h.PID)
	}
	return nil
}

func (a *app) cmdLidwatch(args []string) error {
	if len(args) != 1 {
		return exitf(2, "usage: aisomnia lidwatch start|stop|status|run")
	}
	ag := a.agent()
	switch args[0] {
	case "start":
		if err := (watcher{a}).Start(); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "lid watcher started (%s)\n", lidwatch.Label)
		return nil
	case "stop":
		if err := ag.Stop(); err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, "lid watcher stopped and agent removed")
		return nil
	case "status":
		st := ag.State()
		if st.Loaded {
			fmt.Fprintf(a.stdout, "lid watcher: loaded, state %s, pid %s\n", orDash(st.State), orDash(st.PID))
		} else {
			fmt.Fprintln(a.stdout, "lid watcher: not loaded")
		}
		if line := ag.LastLogLine(); line != "" {
			fmt.Fprintf(a.stdout, "last log line: %s\n", line)
		}
		if active, _ := a.holds().Clean(time.Now(), hold.ProcessAlive); hold.Has(active, hold.Lid) {
			fmt.Fprintln(a.stdout, "paused: a lid hold is active")
		}
		if !st.Loaded {
			return exitf(1, "")
		}
		return nil
	case "run":
		lidHold := func() bool {
			active, _ := a.holds().Clean(time.Now(), hold.ProcessAlive)
			return hold.Has(active, hold.Lid)
		}
		return ag.Run(time.Duration(a.profile.Lidwatch.PollSeconds)*time.Second, lidHold, a.stdout)
	}
	return exitf(2, "lidwatch: unknown action %q", args[0])
}

func (a *app) cmdProfile(args []string) error {
	var initCopy, showPath, force bool
	for _, arg := range args {
		switch arg {
		case "--init":
			initCopy = true
		case "--path":
			showPath = true
		case "--force":
			force = true
		default:
			return exitf(2, "profile: unknown option %s (--init, --path, --force)", arg)
		}
	}
	switch {
	case showPath:
		if a.profilePath == "" {
			fmt.Fprintln(a.stdout, "(built-in)")
		} else {
			fmt.Fprintln(a.stdout, a.profilePath)
		}
	case initCopy:
		target := profile.UserPath(a.home)
		if _, err := os.Stat(target); err == nil && !force {
			return exitf(1, "%s exists; pass --force to overwrite it", target)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(profile.DefaultTOML()), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s — edit it, then run `aisomnia status` and `aisomnia apply`\n", target)
	default:
		fmt.Fprint(a.stdout, a.profile.Source)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
