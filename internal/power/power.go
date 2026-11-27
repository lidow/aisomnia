// Package power reaches a power state now: display off and lock, sleep, a scheduled wake, or one
// deep sleep (hibernate).
package power

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/lidow/aisomnia/internal/macos"
)

// ErrSudoDeclined means the user declined the sudo prompt; nothing changed.
var ErrSudoDeclined = errors.New("sudo declined; nothing changed")

// Lock turns the display off; with the lock delay "immediate" the screen locks and, with idle sleep
// off, the Mac stays awake.
func Lock(r macos.Runner) error {
	_, err := r.Run("pmset", "displaysleepnow")
	return err
}

// Sleep sleeps the Mac now.
func Sleep(r macos.Runner) error {
	_, err := r.Run("pmset", "sleepnow")
	return err
}

func sudo(r macos.Runner, name string, args ...string) error {
	if err := r.RunInteractive("sudo", "-v"); err != nil {
		return ErrSudoDeclined
	}
	_, err := r.Run("sudo", append([]string{"-n", name}, args...)...)
	return err
}

// WakeAt schedules a wake at t (local time).
func WakeAt(r macos.Runner, t, now time.Time) error {
	if !t.After(now) {
		return fmt.Errorf("%s is in the past", t.Format("2006-01-02 15:04"))
	}
	return sudo(r, "pmset", "schedule", "wake", t.Format("01/02/06 15:04:05"))
}

// WakeIn schedules a wake after d.
func WakeIn(r macos.Runner, d time.Duration) error {
	return sudo(r, "pmset", "relative", "wake", strconv.Itoa(int(d.Seconds())))
}

// MarkerPath is the file that says hibernatemode must be restored to the value it holds.
func MarkerPath(stateDir string) string { return filepath.Join(stateDir, "hibernate.restore") }

// ClearMarker drops the marker; apply calls it when it restores hibernatemode anyway.
func ClearMarker(stateDir string) { _ = os.Remove(MarkerPath(stateDir)) }

// Hibernate sets hibernatemode 25 for one sleep and restores restoreMode after wake. The marker
// lets the next `apply` restore it when this process does not survive (hibernatemode then drifts).
func Hibernate(r macos.Runner, restoreMode int, stateDir string, out io.Writer) error {
	restore := strconv.Itoa(restoreMode)
	if restoreMode == 0 {
		restore = "3"
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	if err := r.RunInteractive("sudo", "-v"); err != nil {
		return ErrSudoDeclined
	}
	if err := os.WriteFile(MarkerPath(stateDir), []byte(restore+"\n"), 0o644); err != nil {
		return err
	}
	if _, err := r.Run("sudo", "-n", "pmset", "-a", "hibernatemode", "25"); err != nil {
		return err
	}
	fmt.Fprintln(out, "hibernatemode 25 set; sleeping now. The profile value is restored after wake (or by the next apply).")
	if err := Sleep(r); err != nil {
		return err
	}
	waitForWake(12 * time.Hour)
	if _, err := r.Run("sudo", "-n", "pmset", "-a", "hibernatemode", restore); err != nil {
		// Credentials may have expired during a long sleep: ask again.
		if err2 := r.RunInteractive("sudo", "pmset", "-a", "hibernatemode", restore); err2 != nil {
			return fmt.Errorf("hibernatemode not restored (%v); run `aisomnia apply`", err)
		}
	}
	ClearMarker(stateDir)
	fmt.Fprintf(out, "awake: hibernatemode %s restored\n", restore)
	return nil
}

// waitForWake returns when the wall clock jumps (the process was frozen during sleep) or after limit.
var waitForWake = func(limit time.Duration) {
	start := time.Now()
	for time.Since(start) < limit {
		before := time.Now()
		time.Sleep(5 * time.Second)
		if time.Since(before) > 20*time.Second {
			return
		}
	}
}
