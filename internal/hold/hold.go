// Package hold implements one-time, self-expiring exceptions to the profile: keep the display on,
// keep the screen unlocked, keep the Mac awake with the lid closed, or skip standby. Each active
// hold is a JSON file; the process that holds the state (caffeinate, or the root helper started
// through sudo) is recorded so a hold can be released and stale files cleaned up.
package hold

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

// Kinds of holds.
const (
	Display = "display"
	Lock    = "lock"
	Lid     = "lid"
	Standby = "standby"
)

// Hold is one active exception.
type Hold struct {
	Kind    string    `json:"kind"`
	Started time.Time `json:"started"`
	Expires time.Time `json:"expires"`
	PID     int       `json:"pid"` // process to signal to end the hold
	File    string    `json:"-"`
}

// Expired reports whether the hold's time is up.
func (h Hold) Expired(now time.Time) bool { return !now.Before(h.Expires) }

// Store keeps hold files in one directory.
type Store struct{ Dir string }

// Read returns every hold file in the store, oldest first; unreadable files are skipped.
func (s Store) Read() []Hold {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil
	}
	var holds []Hold
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.Dir, e.Name())
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

// Write stores a hold and returns its file path.
func (s Store) Write(h Hold) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(s.Dir, fmt.Sprintf("%s-%d.json", h.Kind, h.Started.Unix()))
	b, _ := json.MarshalIndent(h, "", "  ")
	return path, os.WriteFile(path, b, 0o644)
}

// Clean removes stale hold files (expired, or whose process is gone) and returns the active
// holds and the removed ones.
func (s Store) Clean(now time.Time, alive func(pid int) bool) (active, removed []Hold) {
	active, removed = Split(s.Read(), now, alive)
	for _, h := range removed {
		_ = os.Remove(h.File)
	}
	return active, removed
}

// Split separates live holds from stale ones.
func Split(holds []Hold, now time.Time, alive func(pid int) bool) (active, stale []Hold) {
	for _, h := range holds {
		if h.Expired(now) || !alive(h.PID) {
			stale = append(stale, h)
		} else {
			active = append(active, h)
		}
	}
	return active, stale
}

// Has reports whether a hold of the kind is among holds.
func Has(holds []Hold, kind string) bool {
	for _, h := range holds {
		if h.Kind == kind {
			return true
		}
	}
	return false
}

// ProcessAlive reports whether pid runs (a permission error still means it exists).
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || os.IsPermission(err)
}

// Plan is how a hold is held: the command to run and what it means for the user.
type Plan struct {
	Argv []string
	What string
	Sudo bool // the command asks for the password in the terminal
}

// PlanFor returns the command for a hold. exe is this binary (the root helper is `exe _hold-run`).
func PlanFor(kind string, d time.Duration, exe string, batteryFloor int) (Plan, error) {
	secs := strconv.Itoa(int(d.Seconds()))
	floor := strconv.Itoa(batteryFloor)
	switch kind {
	case Display:
		return Plan{Argv: []string{"caffeinate", "-d", "-t", secs}, What: "display stays on"}, nil
	case Lock:
		return Plan{Argv: []string{"caffeinate", "-u", "-d", "-t", secs}, What: "user counts as active: no screen saver, no display sleep, no lock"}, nil
	case Lid:
		return Plan{Argv: []string{"sudo", exe, "_hold-run", Lid, secs, floor}, Sudo: true,
			What: "lid close does not sleep the Mac (disablesleep 1; the lid watcher pauses); ends at " + floor + "% battery"}, nil
	case Standby:
		return Plan{Argv: []string{"sudo", exe, "_hold-run", Standby, secs, floor}, Sudo: true, What: "no standby (deep sleep) during sleep"}, nil
	}
	return Plan{}, fmt.Errorf("unknown kind %q (display, lock, lid, standby)", kind)
}
