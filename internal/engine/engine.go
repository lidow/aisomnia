// Package engine compares the declared profile with the live state of the Mac and turns the drift
// into an ordered list of commands: pmset first, then preference plists, then the screen saver,
// then the screen lock, then the lid watcher. Execute runs that list with one sudo prompt.
package engine

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lidow/aisomnia/internal/macos"
	"github.com/lidow/aisomnia/internal/profile"
)

// Row states.
const (
	StateOK          = "ok"
	StateDrift       = "drift"
	StateUnsupported = "unsupported"
)

const (
	hintRestart = "restart the Mac for \"Sleep On Power Button\" to take effect"
	hintRelogin = "log out and in for PowerButtonSleepsSystem to take effect"
)

// Row is one managed setting in the status table.
type Row struct {
	Setting  string `json:"setting"`
	Declared string `json:"declared"`
	Live     string `json:"live"`
	State    string `json:"state"` // ok | drift | unsupported
	Note     string `json:"note,omitempty"`
}

// Command is one step of apply or reset. A step with Internal runs inside the process.
type Command struct {
	Argv        []string
	Sudo        bool
	Interactive bool
	Internal    func() error
	Setting     string
	Hint        string
}

// String renders the command as the user would type it.
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

// Watcher starts or stops the lid watcher; apply and reset call it through internal commands.
type Watcher interface {
	Start() error
	Stop() error
}

// Assess returns the status rows and the commands that remove the drift. lidHold reports an active
// lid hold, during which SleepDisabled 1 is expected and not drift.
func Assess(p *profile.Profile, live *macos.Live, lidHold bool, w Watcher) ([]Row, []Command) {
	var rows []Row
	var cmds []Command

	for _, src := range []struct{ name, flag string }{{"battery", "-b"}, {"ac", "-c"}} {
		declared := p.Pmset.Battery
		if src.name == "ac" {
			declared = p.Pmset.AC
		}
		liveSrc := live.Pmset[src.name]
		for _, key := range profile.OrderedKeys(declared) {
			want := strconv.Itoa(declared[key])
			have, ok := liveSrc[key]
			row := Row{Setting: "pmset " + src.name + " " + key, Declared: want, Live: have}
			switch {
			case !ok:
				row.Live, row.State, row.Note = "-", StateUnsupported, "not listed by pmset -g custom on this Mac; skipped"
			case have == want:
				row.State = StateOK
			default:
				row.State = StateDrift
				cmds = append(cmds, Command{Argv: []string{"pmset", src.flag, key, want}, Sudo: true, Setting: row.Setting})
			}
			rows = append(rows, row)
		}
		// Safety net for a lid or standby hold that did not revert (crash, lost terminal).
		if v, ok := liveSrc["SleepDisabled"]; ok && v == "1" && !lidHold {
			rows = append(rows, Row{Setting: "pmset " + src.name + " disablesleep", Declared: "0", Live: "1", State: StateDrift,
				Note: "left behind by a lid hold; apply reverts it"})
			if src.name == "battery" { // one -a command covers both sources
				cmds = append(cmds, Command{Argv: []string{"pmset", "-a", "disablesleep", "0"}, Sudo: true, Setting: "disablesleep"})
			}
		}
	}

	// Power-button keys: declared nil means "must be absent".
	rows, cmds = plistRow(rows, cmds, "power button: Sleep On Power Button", intPtrString(p.PowerButton.SleepOnPowerButton),
		live.SleepOnPowerButton, macos.PowerManagementPlist, macos.SleepOnPowerButton, "-int", hintRestart)
	rows, cmds = plistRow(rows, cmds, "power button: loginwindow PowerButtonSleepsSystem", boolPtrString(p.PowerButton.LoginwindowSleepsSystem),
		live.LoginwindowSleeps, macos.LoginwindowPlist, macos.PowerButtonSleeps, "-bool", hintRelogin)

	// Screen saver idle time (seconds), per host, no sudo.
	want := strconv.Itoa(p.Screen.SaverIdleSeconds)
	row := Row{Setting: "screen saver idle (s)", Declared: want, Live: ptrString(live.SaverIdle, "unset")}
	if live.SaverIdle != nil && *live.SaverIdle == want {
		row.State = StateOK
	} else {
		row.State = StateDrift
		cmds = append(cmds, Command{Argv: []string{"defaults", "-currentHost", "write", macos.ScreensaverDomain, macos.ScreensaverIdleKey, "-int", want}, Setting: row.Setting})
	}
	rows = append(rows, row)

	// Lock delay: sysadminctl asks the user for the login password (never scripted).
	row = Row{Setting: "screen lock delay", Declared: p.Screen.LockDelay, Live: live.LockDelay}
	if live.LockDelay == p.Screen.LockDelay {
		row.State = StateOK
	} else {
		row.State = StateDrift
		row.Note = "sysadminctl asks for your login password"
		cmds = append(cmds, Command{Argv: []string{"sysadminctl", "-screenLock", p.Screen.LockDelay, "-password", "-"}, Interactive: true, Setting: row.Setting})
	}
	rows = append(rows, row)

	// Lid watcher agent.
	wantAgent, haveAgent := "stopped", "stopped"
	if p.Lidwatch.Enabled {
		wantAgent = "running"
	}
	if live.Agent.Loaded {
		haveAgent = "running"
		if live.Agent.PID != "" {
			haveAgent += " (pid " + live.Agent.PID + ")"
		}
	}
	row = Row{Setting: "lid watcher (launchd agent)", Declared: wantAgent, Live: haveAgent}
	if live.Agent.Loaded == p.Lidwatch.Enabled {
		row.State = StateOK
	} else {
		row.State = StateDrift
		if p.Lidwatch.Enabled {
			cmds = append(cmds, Command{Argv: []string{"lidwatch", "start"}, Internal: w.Start, Setting: row.Setting})
		} else {
			cmds = append(cmds, Command{Argv: []string{"lidwatch", "stop"}, Internal: w.Stop, Setting: row.Setting})
		}
	}
	rows = append(rows, row)
	return rows, cmds
}

func plistRow(rows []Row, cmds []Command, setting string, want *string, have *string, domain, key, typ, hint string) ([]Row, []Command) {
	row := Row{Setting: setting, Declared: ptrString(want, "unset"), Live: ptrString(have, "unset")}
	switch {
	case want == nil && have == nil, want != nil && have != nil && *want == *have:
		row.State = StateOK
	case want == nil:
		row.State, row.Note = StateDrift, hint
		cmds = append(cmds, Command{Argv: []string{"defaults", "delete", domain, key}, Sudo: true, Setting: setting, Hint: hint})
	default:
		row.State, row.Note = StateDrift, hint
		cmds = append(cmds, Command{Argv: []string{"defaults", "write", domain, key, typ, writeValue(typ, *want)}, Sudo: true, Setting: setting, Hint: hint})
	}
	return append(rows, row), cmds
}

// writeValue renders a value for `defaults write`: `-bool` wants true/false although
// `defaults read` prints 0/1.
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

// boolPtrString renders a bool the way `defaults read` prints it (0/1).
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

// HasDrift reports whether any row drifts.
func HasDrift(rows []Row) bool {
	for _, r := range rows {
		if r.State == StateDrift {
			return true
		}
	}
	return false
}

// BatteryWarning is the line printed under the status table on battery with idle sleep off.
func BatteryWarning(p *profile.Profile, live *macos.Live) string {
	if live.OnBattery && p.Pmset.Battery["sleep"] == 0 {
		return fmt.Sprintf("on battery (%d%%) with idle sleep off: close the lid to sleep, or the battery drains while locked", live.BatteryPercent)
	}
	return ""
}
