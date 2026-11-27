package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

// Hold is a one-time, self-expiring exception, one JSON file per hold.
type Hold struct {
	Kind    string    `json:"kind"` // display | lock | lid | standby
	Started time.Time `json:"started"`
	Expires time.Time `json:"expires"`
	PID     int       `json:"pid"` // process to signal to end the hold (caffeinate, or sudo running _hold-run)
	File    string    `json:"-"`
}

func (h Hold) Expired(now time.Time) bool { return !now.Before(h.Expires) }

func (a *app) holdsDir() string { return filepath.Join(a.stateDir(), "holds") }

func (a *app) readHolds() []Hold {
	entries, err := os.ReadDir(a.holdsDir())
	if err != nil {
		return nil
	}
	var holds []Hold
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(a.holdsDir(), e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var h Hold
		if json.Unmarshal(b, &h) != nil {
			continue
		}
		h.File = path
		holds = append(holds, h)
	}
	sort.Slice(holds, func(i, j int) bool { return holds[i].Started.Before(holds[j].Started) })
	return holds
}

func (a *app) writeHold(h Hold) (string, error) {
	if err := os.MkdirAll(a.holdsDir(), 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(a.holdsDir(), fmt.Sprintf("%s-%d.json", h.Kind, h.Started.Unix()))
	b, _ := json.MarshalIndent(h, "", "  ")
	return path, os.WriteFile(path, b, 0o644)
}

// splitHolds separates live holds from stale ones (expired, or whose process is gone).
func splitHolds(holds []Hold, now time.Time, alive func(pid int) bool) (active, stale []Hold) {
	for _, h := range holds {
		if h.Expired(now) || !alive(h.PID) {
			stale = append(stale, h)
		} else {
			active = append(active, h)
		}
	}
	return active, stale
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil || os.IsPermission(syscall.Kill(pid, 0))
}

// cleanHolds removes stale hold files and returns the active holds and the removed ones.
func (a *app) cleanHolds() (active, removed []Hold) {
	active, removed = splitHolds(a.readHolds(), time.Now(), processAlive)
	for _, h := range removed {
		_ = os.Remove(h.File)
	}
	return active, removed
}

func hasHold(holds []Hold, kind string) bool {
	for _, h := range holds {
		if h.Kind == kind {
			return true
		}
	}
	return false
}

// cmdHold runs in the foreground until the duration ends, Ctrl-C, or the terminal closes.
func (a *app) cmdHold(args []string) error {
	if len(args) != 2 {
		return exitf(2, "usage: aisomnia hold display|lock|lid|standby <duration>")
	}
	kind := args[0]
	d, err := parseDuration(args[1])
	if err != nil {
		return exitf(2, "hold: %v", err)
	}
	secs := strconv.Itoa(int(d.Seconds()))
	var cmd *exec.Cmd
	var what string
	switch kind {
	case "display":
		cmd = exec.Command("caffeinate", "-d", "-t", secs)
		what = "display stays on"
	case "lock":
		cmd = exec.Command("caffeinate", "-u", "-d", "-t", secs)
		what = "user counts as active: no screen saver, no display sleep, no lock"
	case "lid", "standby":
		active, _ := a.cleanHolds()
		if hasHold(active, kind) {
			return exitf(1, "a %s hold is already active; `aisomnia holds` lists it", kind)
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		floor := strconv.Itoa(a.profile.Lidwatch.BatteryFloorPercent)
		cmd = exec.Command("sudo", exe, "_hold-run", kind, secs, floor)
		cmd.Stdin = os.Stdin
		if kind == "lid" {
			what = "lid close does not sleep the Mac (disablesleep 1; the lid watcher pauses); ends at " + floor + "% battery"
		} else {
			what = "no standby (deep sleep) during sleep"
		}
	default:
		return exitf(2, "hold: unknown kind %q (display, lock, lid, standby)", kind)
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	h := Hold{Kind: kind, Started: time.Now(), Expires: time.Now().Add(d), PID: cmd.Process.Pid}
	path, err := a.writeHold(h)
	if err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return err
	}
	defer func() { _ = os.Remove(path) }()
	fmt.Fprintf(a.stdout, "hold %s for %s (until %s): %s. Ctrl-C ends it early.\n", kind, d, h.Expires.Format("15:04:05"), what)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-sig:
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = <-done
		fmt.Fprintf(a.stdout, "hold %s released\n", kind)
		return nil
	case err := <-done:
		if err != nil && !isKilled(err) {
			return fmt.Errorf("hold %s ended with an error: %w", kind, err)
		}
		fmt.Fprintf(a.stdout, "hold %s ended\n", kind)
		return nil
	}
}

func isKilled(err error) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			return ws.Signaled()
		}
	}
	return false
}

// cmdHoldRun is the root helper behind `hold lid|standby`: it sets the pmset value, waits for the
// duration, a signal, or the battery floor (lid), and always reverts. Started only through sudo.
func (a *app) cmdHoldRun(args []string) error {
	if os.Geteuid() != 0 {
		return exitf(2, "_hold-run is started by `aisomnia hold` through sudo")
	}
	if len(args) != 3 {
		return exitf(2, "usage: _hold-run lid|standby <seconds> <battery floor>")
	}
	kind := args[0]
	secs, _ := strconv.Atoi(args[1])
	floor, _ := strconv.Atoi(args[2])
	var set, revert []string
	switch kind {
	case "lid":
		set, revert = []string{"-a", "disablesleep", "1"}, []string{"-a", "disablesleep", "0"}
	case "standby":
		set, revert = []string{"-a", "standby", "0"}, []string{"-a", "standby", strconv.Itoa(a.profile.Pmset.AC["standby"])}
	default:
		return exitf(2, "_hold-run: unknown kind %q", kind)
	}
	if _, err := a.runner.Run("pmset", set...); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "pmset %v set; reverting in %ds\n", set, secs)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	reason := "duration ended"
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
loop:
	for {
		select {
		case <-sig:
			reason = "released"
			break loop
		case <-tick.C:
			if !time.Now().Before(deadline) {
				break loop
			}
			if kind == "lid" {
				out, _ := a.runner.Run("pmset", "-g", "batt")
				if onBatt, pct := parseBattery(out); onBatt && pct >= 0 && pct <= floor {
					reason = fmt.Sprintf("battery at %d%% (floor %d%%)", pct, floor)
					break loop
				}
			}
		}
	}
	if _, err := a.runner.Run("pmset", revert...); err != nil {
		return fmt.Errorf("revert failed (%v); run `aisomnia apply`", err)
	}
	fmt.Fprintf(a.stdout, "pmset %v reverted: %s\n", revert, reason)
	return nil
}

func (a *app) cmdHolds() error {
	active, removed := a.cleanHolds()
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

func (a *app) cmdRelease() error {
	active, _ := a.cleanHolds()
	if len(active) == 0 {
		fmt.Fprintln(a.stdout, "no active holds")
		return nil
	}
	for _, h := range active {
		if err := syscall.Kill(h.PID, syscall.SIGTERM); err != nil {
			fmt.Fprintf(a.stdout, "%s hold (pid %d): %v\n", h.Kind, h.PID, err)
		} else {
			fmt.Fprintf(a.stdout, "%s hold released\n", h.Kind)
		}
	}
	// Give the owning `aisomnia hold` processes a moment to revert and remove their files.
	time.Sleep(500 * time.Millisecond)
	for _, h := range active {
		if !processAlive(h.PID) {
			_ = os.Remove(h.File)
		}
	}
	return nil
}
