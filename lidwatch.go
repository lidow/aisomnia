package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed launchd/dev.aisomnia.lidwatch.plist
var agentPlistTemplate string

const logMaxBytes = 1 << 20 // rotate at 1 MB

// watcher decides when a lid close must sleep the Mac. It is pure so tests can
// drive it: a transition open→closed sleeps; a wake with the lid still closed (an external keyboard
// woke the Mac in clamshell) sleeps again; an active lid hold pauses it. The first observation
// never sleeps, so a login with the lid already closed is left alone.
type watcher struct {
	poll       time.Duration
	known      bool
	prevClosed bool
	lastTick   time.Time
}

func (w *watcher) step(closed bool, now time.Time, lidHold bool) (sleep bool, reason string) {
	woke := w.known && !w.lastTick.IsZero() && now.Sub(w.lastTick) > 3*w.poll+5*time.Second
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

func (a *app) logDir() string  { return filepath.Join(a.home, "Library", "Logs", appName) }
func (a *app) logPath() string { return filepath.Join(a.logDir(), "aisomnia.log") }
func (a *app) agentLogPath() string {
	return filepath.Join(a.logDir(), "aisomnia-agent.log")
}
func (a *app) agentPlistPath() string {
	return filepath.Join(a.home, "Library", "LaunchAgents", agentLabel+".plist")
}

func (a *app) logLine(format string, args ...any) {
	_ = os.MkdirAll(a.logDir(), 0o755)
	if st, err := os.Stat(a.logPath()); err == nil && st.Size() > logMaxBytes {
		_ = os.Rename(a.logPath(), a.logPath()+".1")
	}
	f, err := os.OpenFile(a.logPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

func (a *app) cmdLidwatch(args []string) error {
	if len(args) != 1 {
		return exitf(2, "usage: aisomnia lidwatch start|stop|status|run")
	}
	switch args[0] {
	case "start":
		if err := a.lidwatchStart(); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "lid watcher started (%s)\n", agentLabel)
		return nil
	case "stop":
		if err := a.lidwatchStop(); err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, "lid watcher stopped and agent removed")
		return nil
	case "status":
		return a.lidwatchStatus()
	case "run":
		return a.lidwatchRun()
	}
	return exitf(2, "lidwatch: unknown action %q", args[0])
}

func (a *app) lidwatchStart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	_ = os.MkdirAll(a.logDir(), 0o755)
	profileArgs := ""
	if a.profilePath != "" {
		abs, err := filepath.Abs(a.profilePath)
		if err == nil {
			profileArgs = "\t\t<string>--profile</string>\n\t\t<string>" + xmlEscape(abs) + "</string>\n"
		}
	}
	plist := strings.NewReplacer("{{EXE}}", xmlEscape(exe), "{{PROFILE_ARGS}}", profileArgs, "{{AGENTLOG}}", xmlEscape(a.agentLogPath())).Replace(agentPlistTemplate)
	if err := os.MkdirAll(filepath.Dir(a.agentPlistPath()), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(a.agentPlistPath(), []byte(plist), 0o644); err != nil {
		return err
	}
	target := "gui/" + currentUID()
	// Reload if already loaded so a new binary path or profile takes effect.
	if _, err := a.runner.Run("launchctl", "print", target+"/"+agentLabel); err == nil {
		_, _ = a.runner.Run("launchctl", "bootout", target+"/"+agentLabel)
	}
	if _, err := a.runner.Run("launchctl", "bootstrap", target, a.agentPlistPath()); err != nil {
		return err
	}
	return nil
}

func (a *app) lidwatchStop() error {
	target := "gui/" + currentUID() + "/" + agentLabel
	if _, err := a.runner.Run("launchctl", "bootout", target); err != nil && !strings.Contains(err.Error(), "No such process") && !strings.Contains(err.Error(), "Could not find") {
		return err
	}
	if err := os.Remove(a.agentPlistPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *app) lidwatchStatus() error {
	st := parseLaunchctlPrint(a.runner.Run("launchctl", "print", "gui/"+currentUID()+"/"+agentLabel))
	if !st.Loaded {
		fmt.Fprintln(a.stdout, "lid watcher: not loaded")
	} else {
		fmt.Fprintf(a.stdout, "lid watcher: loaded, state %s, pid %s\n", orDash(st.State), orDash(st.PID))
	}
	if b, err := os.ReadFile(a.logPath()); err == nil {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		if n := len(lines); n > 0 && lines[n-1] != "" {
			fmt.Fprintf(a.stdout, "last log line: %s\n", lines[n-1])
		}
	}
	active, _ := a.cleanHolds()
	if hasHold(active, "lid") {
		fmt.Fprintln(a.stdout, "paused: a lid hold is active")
	}
	if !st.Loaded {
		return exitf(1, "")
	}
	return nil
}

// lidwatchRun is the daemon loop launchd runs.
func (a *app) lidwatchRun() error {
	poll := time.Duration(a.profile.Lidwatch.PollSeconds) * time.Second
	w := &watcher{poll: poll}
	a.logLine("lidwatch start (poll %s, pid %d)", poll, os.Getpid())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case s := <-sig:
			a.logLine("lidwatch stop (%s)", s)
			return nil
		case <-tick.C:
		}
		out, err := a.runner.Run("ioreg", "-r", "-k", "AppleClamshellState", "-d", "4")
		if err != nil {
			a.logLine("ioreg: %v", err)
			continue
		}
		closed, ok := parseClamshell(out)
		if !ok {
			continue
		}
		active, _ := a.cleanHolds()
		if sleep, reason := w.step(closed, time.Now(), hasHold(active, "lid")); sleep {
			if _, err := a.runner.Run("pmset", "sleepnow"); err != nil {
				a.logLine("sleepnow failed: %v", err)
			} else {
				a.logLine("sleep: %s", reason)
			}
		}
	}
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
