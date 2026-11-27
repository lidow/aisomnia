// Package lidwatch sleeps the Mac when the lid closes, also in clamshell mode (external display and
// power attached), where macOS alone keeps it awake. It runs as a launchd user agent that polls the
// clamshell sensor.
package lidwatch

import "time"

// Label is the launchd label of the agent.
const Label = "dev.aisomnia.lidwatch"

// Watcher decides when a lid close must sleep the Mac. It is pure so tests can drive it: a
// transition open→closed sleeps; a wake with the lid still closed (an external keyboard woke the
// Mac in clamshell) sleeps again; an active lid hold pauses it. The first observation never
// sleeps, so a login with the lid already closed is left alone.
type Watcher struct {
	Poll       time.Duration
	known      bool
	prevClosed bool
	lastTick   time.Time
}

// Step feeds one observation and returns whether to sleep now, and why.
func (w *Watcher) Step(closed bool, now time.Time, lidHold bool) (sleep bool, reason string) {
	woke := w.known && !w.lastTick.IsZero() && now.Sub(w.lastTick) > 3*w.Poll+5*time.Second
	wasKnown, wasClosed := w.known, w.prevClosed
	w.known, w.prevClosed, w.lastTick = true, closed, now
	switch {
	case lidHold, !closed, !wasKnown:
		return false, ""
	case !wasClosed:
		return true, "lid closed"
	case woke:
		return true, "woke with the lid closed"
	}
	return false, ""
}
