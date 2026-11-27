package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cmdForce reaches a state now: lock/display-off, sleep, hibernate, wake-at, wake-in.
func (a *app) cmdForce(verb string, args []string) error {
	switch verb {
	case "lock", "display-off":
		if len(args) != 0 {
			return exitf(2, "%s takes no arguments", verb)
		}
		// Display off + lock delay "immediate" = locked; sleep 0 keeps the Mac awake.
		_, err := a.runner.Run("pmset", "displaysleepnow")
		return err
	case "sleep":
		if len(args) != 0 {
			return exitf(2, "sleep takes no arguments")
		}
		_, err := a.runner.Run("pmset", "sleepnow")
		return err
	case "wake-at":
		if len(args) != 1 {
			return exitf(2, `usage: aisomnia wake-at "YYYY-MM-DD HH:MM"`)
		}
		t, err := time.ParseInLocation("2006-01-02 15:04", args[0], time.Local)
		if err != nil {
			return exitf(2, "wake-at: %v", err)
		}
		if !t.After(time.Now()) {
			return exitf(2, "wake-at: %s is in the past", args[0])
		}
		return a.sudoRun("pmset", "schedule", "wake", t.Format("01/02/06 15:04:05"))
	case "wake-in":
		if len(args) != 1 {
			return exitf(2, "usage: aisomnia wake-in <duration>, e.g. 2h or 90m")
		}
		d, err := parseDuration(args[0])
		if err != nil {
			return exitf(2, "wake-in: %v", err)
		}
		return a.sudoRun("pmset", "relative", "wake", strconv.Itoa(int(d.Seconds())))
	case "hibernate":
		if len(args) != 0 {
			return exitf(2, "hibernate takes no arguments")
		}
		return a.hibernate()
	}
	return exitf(2, "unknown verb %s", verb)
}

// sudoRun asks for the password through the terminal, then runs one root command.
func (a *app) sudoRun(name string, args ...string) error {
	if err := a.runner.RunInteractive("sudo", "-v"); err != nil {
		return exitf(1, "sudo declined; nothing changed")
	}
	_, err := a.runner.Run("sudo", append([]string{"-n", name}, args...)...)
	return err
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

// ----- hibernate (Could) --------------------------------------------------

func (a *app) stateDir() string {
	return filepath.Join(a.home, "Library", "Application Support", appName)
}

func (a *app) hibernateMarker() string { return filepath.Join(a.stateDir(), "hibernate.restore") }

// hibernate sets hibernatemode 25 for one sleep and restores the profile value after wake. A marker
// file makes the next `apply` restore it when the process did not survive (the profile value drifts).
func (a *app) hibernate() error {
	restore := strconv.Itoa(a.profile.Pmset.AC["hibernatemode"])
	if restore == "0" {
		restore = "3"
	}
	if err := os.MkdirAll(a.stateDir(), 0o755); err != nil {
		return err
	}
	if err := a.runner.RunInteractive("sudo", "-v"); err != nil {
		return exitf(1, "sudo declined; nothing changed")
	}
	if err := os.WriteFile(a.hibernateMarker(), []byte(restore+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := a.runner.Run("sudo", "-n", "pmset", "-a", "hibernatemode", "25"); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "hibernatemode 25 set; sleeping now. The profile value is restored after wake (or by the next apply).")
	if _, err := a.runner.Run("pmset", "sleepnow"); err != nil {
		return err
	}
	// The process is frozen during sleep; a wall-clock jump tells us the Mac is back.
	start := time.Now()
	for time.Since(start) < 12*time.Hour {
		before := time.Now()
		time.Sleep(5 * time.Second)
		if time.Since(before) > 20*time.Second {
			break
		}
	}
	if _, err := a.runner.Run("sudo", "-n", "pmset", "-a", "hibernatemode", restore); err != nil {
		// Credentials may have expired during a long sleep: ask again.
		if err2 := a.runner.RunInteractive("sudo", "pmset", "-a", "hibernatemode", restore); err2 != nil {
			return fmt.Errorf("hibernatemode not restored (%v); run `aisomnia apply`", err)
		}
	}
	_ = os.Remove(a.hibernateMarker())
	fmt.Fprintf(a.stdout, "awake: hibernatemode %s restored\n", restore)
	return nil
}

// clearHibernateMarker drops the marker when apply is about to restore hibernatemode anyway.
func (a *app) clearHibernateMarker(cmds []Command) {
	for _, c := range cmds {
		if strings.HasSuffix(c.Setting, "hibernatemode") {
			_ = os.Remove(a.hibernateMarker())
			return
		}
	}
}
