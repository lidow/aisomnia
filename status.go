package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Row is one managed setting in the status table.
type Row struct {
	Setting  string `json:"setting"`
	Declared string `json:"declared"`
	Live     string `json:"live"`
	State    string `json:"state"` // ok | drift | unsupported
	Note     string `json:"note,omitempty"`
}

// Command is one step of apply/reset. Internal steps run inside the process.
type Command struct {
	Argv        []string
	Sudo        bool
	Interactive bool
	Internal    func(a *app) error
	Setting     string
	Hint        string
}

func (c Command) String() string {
	if c.Internal != nil {
		return "aisomnia " + strings.Join(c.Argv, " ")
	}
	s := strings.Join(quoteArgs(c.Argv), " ")
	if c.Sudo {
		return "sudo " + s
	}
	return s
}

func quoteArgs(argv []string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if strings.ContainsAny(a, " \t\"'") {
			out[i] = strconv.Quote(a)
		} else {
			out[i] = a
		}
	}
	return out
}

const (
	stateOK          = "ok"
	stateDrift       = "drift"
	stateUnsupported = "unsupported"
	hintRestart      = "restart the Mac for \"Sleep On Power Button\" to take effect"
	hintRelogin      = "log out and in for PowerButtonSleepsSystem to take effect"
)

// assess compares the profile with the live state and returns the status rows and the ordered
// commands that would remove the drift: pmset, then plists, then screen saver, then lock, then the
// lid watcher. lidHold reports an active lid hold, during which SleepDisabled 1 is expected.
func assess(p *Profile, live *Live, lidHold bool) ([]Row, []Command) {
	var rows []Row
	var cmds []Command

	for _, src := range []struct{ name, flag string }{{"battery", "-b"}, {"ac", "-c"}} {
		declared := p.Pmset.Battery
		if src.name == "ac" {
			declared = p.Pmset.AC
		}
		liveSrc := live.Pmset[src.name]
		for _, key := range orderedKeys(declared) {
			want := strconv.Itoa(declared[key])
			have, ok := liveSrc[key]
			row := Row{Setting: "pmset " + src.name + " " + key, Declared: want, Live: have}
			switch {
			case !ok:
				row.Live, row.State, row.Note = "-", stateUnsupported, "not listed by pmset -g custom on this Mac; skipped"
			case have == want:
				row.State = stateOK
			default:
				row.State = stateDrift
				cmds = append(cmds, Command{Argv: []string{"pmset", src.flag, key, want}, Sudo: true, Setting: row.Setting})
			}
			rows = append(rows, row)
		}
		// Safety net for a lid or standby hold that did not revert (crash, lost terminal).
		if v, ok := liveSrc["SleepDisabled"]; ok && v == "1" && !lidHold {
			rows = append(rows, Row{Setting: "pmset " + src.name + " disablesleep", Declared: "0", Live: "1", State: stateDrift,
				Note: "left behind by a lid hold; apply reverts it"})
			if src.name == "battery" { // one -a command covers both sources
				cmds = append(cmds, Command{Argv: []string{"pmset", "-a", "disablesleep", "0"}, Sudo: true, Setting: "disablesleep"})
			}
		}
	}

	// Power-button keys: declared nil means "must be absent".
	rows, cmds = plistRow(rows, cmds, "power button: Sleep On Power Button", intPtrString(p.PowerButton.SleepOnPowerButton),
		live.SleepOnPowerButton, pmPlist, pmPlistKey, "-int", hintRestart)
	rows, cmds = plistRow(rows, cmds, "power button: loginwindow PowerButtonSleepsSystem", boolPtrString(p.PowerButton.LoginwindowSleepsSystem),
		live.LoginwindowSleeps, loginwindowPlist, loginwindowKey, "-bool", hintRelogin)

	// Screen saver idle time (seconds), per host, no sudo.
	want := strconv.Itoa(p.Screen.SaverIdleSeconds)
	row := Row{Setting: "screen saver idle (s)", Declared: want, Live: ptrString(live.SaverIdle, "unset")}
	if live.SaverIdle != nil && *live.SaverIdle == want {
		row.State = stateOK
	} else {
		row.State = stateDrift
		cmds = append(cmds, Command{Argv: []string{"defaults", "-currentHost", "write", screensaverDom, screensaverKey, "-int", want}, Setting: row.Setting})
	}
	rows = append(rows, row)

	// Lock delay: sysadminctl prompts you for the login password (never scripted).
	row = Row{Setting: "screen lock delay", Declared: p.Screen.LockDelay, Live: live.LockDelay}
	if live.LockDelay == p.Screen.LockDelay {
		row.State = stateOK
	} else {
		row.State = stateDrift
		row.Note = "sysadminctl asks for your login password"
		cmds = append(cmds, Command{Argv: []string{"sysadminctl", "-screenLock", p.Screen.LockDelay, "-password", "-"}, Interactive: true, Setting: row.Setting})
	}
	rows = append(rows, row)

	// Lid watcher agent.
	wantAgent := "stopped"
	if p.Lidwatch.Enabled {
		wantAgent = "running"
	}
	haveAgent := "stopped"
	if live.Agent.Loaded {
		haveAgent = "running"
		if live.Agent.PID != "" {
			haveAgent += " (pid " + live.Agent.PID + ")"
		}
	}
	row = Row{Setting: "lid watcher (launchd agent)", Declared: wantAgent, Live: haveAgent}
	if live.Agent.Loaded == p.Lidwatch.Enabled {
		row.State = stateOK
	} else {
		row.State = stateDrift
		if p.Lidwatch.Enabled {
			cmds = append(cmds, Command{Argv: []string{"lidwatch", "start"}, Internal: func(a *app) error { return a.lidwatchStart() }, Setting: row.Setting})
		} else {
			cmds = append(cmds, Command{Argv: []string{"lidwatch", "stop"}, Internal: func(a *app) error { return a.lidwatchStop() }, Setting: row.Setting})
		}
	}
	rows = append(rows, row)
	return rows, cmds
}

func plistRow(rows []Row, cmds []Command, setting string, want *string, have *string, domain, key, typ, hint string) ([]Row, []Command) {
	row := Row{Setting: setting, Declared: ptrString(want, "unset"), Live: ptrString(have, "unset")}
	switch {
	case want == nil && have == nil:
		row.State = stateOK
	case want != nil && have != nil && *want == *have:
		row.State = stateOK
	case want == nil:
		row.State, row.Note = stateDrift, hint
		cmds = append(cmds, Command{Argv: []string{"defaults", "delete", domain, key}, Sudo: true, Setting: setting, Hint: hint})
	default:
		row.State, row.Note = stateDrift, hint
		cmds = append(cmds, Command{Argv: []string{"defaults", "write", domain, key, typ, writeValue(typ, *want)}, Sudo: true, Setting: setting, Hint: hint})
	}
	return append(rows, row), cmds
}

// writeValue renders a value for `defaults write`: `-bool` wants true/false although `defaults read`
// prints 0/1.
func writeValue(typ, v string) string {
	if typ == "-bool" {
		if v == "1" {
			return "true"
		}
		return "false"
	}
	return v
}

func ptrString(p *string, missing string) string {
	if p == nil {
		return missing
	}
	return *p
}

func intPtrString(p *int) *string {
	if p == nil {
		return nil
	}
	s := strconv.Itoa(*p)
	return &s
}

// boolPtrString renders a bool the way `defaults read` prints it (0/1) and `defaults write -bool` accepts.
func boolPtrString(p *bool) *string {
	if p == nil {
		return nil
	}
	s := "0"
	if *p {
		s = "1"
	}
	return &s
}

func hasDrift(rows []Row) bool {
	for _, r := range rows {
		if r.State == stateDrift {
			return true
		}
	}
	return false
}

// warnings returns the lines printed under the table (battery reminder, removed stale holds).
func warnings(p *Profile, live *Live, expired []Hold) []string {
	var w []string
	if live.OnBattery && p.Pmset.Battery["sleep"] == 0 {
		w = append(w, fmt.Sprintf("on battery (%d%%) with idle sleep off: close the lid to sleep, or the battery drains while locked", live.BatteryPercent))
	}
	for _, h := range expired {
		w = append(w, fmt.Sprintf("expired %s hold from %s removed", h.Kind, h.Started.Format("2006-01-02 15:04")))
	}
	return w
}

type statusReport struct {
	Profile  string   `json:"profile"`
	Rows     []Row    `json:"settings"`
	Drift    bool     `json:"drift"`
	Warnings []string `json:"warnings"`
}

func (a *app) cmdStatus(args []string) error {
	asJSON := false
	for _, arg := range args {
		switch arg {
		case "--json":
			asJSON = true
		default:
			return exitf(2, "status: unknown option %s", arg)
		}
	}
	live, err := readLive(a.runner, currentUID())
	if err != nil {
		return err
	}
	active, expired := a.cleanHolds()
	rows, _ := assess(a.profile, live, hasHold(active, "lid"))
	rep := statusReport{Profile: a.profile.Name, Rows: rows, Drift: hasDrift(rows), Warnings: warnings(a.profile, live, expired)}
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
