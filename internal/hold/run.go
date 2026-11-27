package hold

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/lidow/aisomnia/internal/macos"
)

// Hold runs plan in the foreground until it ends, Ctrl-C, or the terminal closes, and keeps a hold
// file for it in store while it runs.
func (s Store) Hold(kind string, d time.Duration, plan Plan, out io.Writer) error {
	cmd := exec.Command(plan.Argv[0], plan.Argv[1:]...)
	if plan.Sudo {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	h := Hold{Kind: kind, Started: time.Now(), Expires: time.Now().Add(d), PID: cmd.Process.Pid}
	path, err := s.Write(h)
	if err != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		return err
	}
	defer func() { _ = os.Remove(path) }()
	fmt.Fprintf(out, "hold %s for %s (until %s): %s. Ctrl-C ends it early.\n", kind, d, h.Expires.Format("15:04:05"), plan.What)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-sig:
		_ = cmd.Process.Signal(syscall.SIGTERM)
		<-done //nolint:errcheck // the hold process was just signaled; its exit error is expected
		fmt.Fprintf(out, "hold %s released\n", kind)
		return nil
	case err := <-done:
		if err != nil && !isKilled(err) {
			return fmt.Errorf("hold %s ended with an error: %w", kind, err)
		}
		fmt.Fprintf(out, "hold %s ended\n", kind)
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

// Release sends SIGTERM to every active hold and removes the files of those that ended.
func (s Store) Release(out io.Writer) {
	active, _ := s.Clean(time.Now(), ProcessAlive)
	if len(active) == 0 {
		fmt.Fprintln(out, "no active holds")
		return
	}
	for _, h := range active {
		if err := syscall.Kill(h.PID, syscall.SIGTERM); err != nil {
			fmt.Fprintf(out, "%s hold (pid %d): %v\n", h.Kind, h.PID, err)
		} else {
			fmt.Fprintf(out, "%s hold released\n", h.Kind)
		}
	}
	// Give the owning `aisomnia hold` processes a moment to revert and remove their files.
	time.Sleep(500 * time.Millisecond)
	for _, h := range active {
		if !ProcessAlive(h.PID) {
			_ = os.Remove(h.File)
		}
	}
}

// Tick is how often the root helper checks its deadline and the battery (tests shorten it).
var Tick = 5 * time.Second

// RootHelper is `aisomnia _hold-run`, started through sudo by Hold for lid and standby holds: it
// sets one pmset value, waits for the duration, a signal or (lid) the battery floor, and always
// reverts. standbyRestore is the profile's standby value.
func RootHelper(r macos.Runner, kind string, secs, floor, standbyRestore int, out io.Writer, stop <-chan os.Signal) error {
	var set, revert []string
	switch kind {
	case Lid:
		set, revert = []string{"-a", "disablesleep", "1"}, []string{"-a", "disablesleep", "0"}
	case Standby:
		set, revert = []string{"-a", "standby", "0"}, []string{"-a", "standby", strconv.Itoa(standbyRestore)}
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
	if _, err := r.Run("pmset", set...); err != nil {
		return err
	}
	fmt.Fprintf(out, "pmset %v set; reverting in %ds\n", set, secs)
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	reason := "duration ended"
	tick := time.NewTicker(Tick)
	defer tick.Stop()
loop:
	for {
		select {
		case <-stop:
			reason = "released"
			break loop
		case <-tick.C:
			if !time.Now().Before(deadline) {
				break loop
			}
			if kind == Lid {
				o, _ := r.Run("pmset", "-g", "batt")
				if onBatt, pct := macos.ParseBattery(o); onBatt && pct >= 0 && pct <= floor {
					reason = fmt.Sprintf("battery at %d%% (floor %d%%)", pct, floor)
					break loop
				}
			}
		}
	}
	if _, err := r.Run("pmset", revert...); err != nil {
		return fmt.Errorf("revert failed (%v); run `aisomnia apply`", err)
	}
	fmt.Fprintf(out, "pmset %v reverted: %s\n", revert, reason)
	return nil
}
