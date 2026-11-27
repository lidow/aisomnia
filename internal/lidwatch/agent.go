package lidwatch

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lidow/aisomnia/internal/macos"
)

//go:embed launchd.plist
var plistTemplate string

const logMaxBytes = 1 << 20 // rotate at 1 MB

// Agent manages the launchd agent and its log.
type Agent struct {
	Runner    macos.Runner
	UID       string
	PlistPath string // ~/Library/LaunchAgents/<Label>.plist
	LogDir    string // ~/Library/Logs/aisomnia
}

func (a Agent) target() string  { return "gui/" + a.UID }
func (a Agent) service() string { return a.target() + "/" + Label }

// LogPath is the watcher's own log (one line per start, stop and sleep).
func (a Agent) LogPath() string { return filepath.Join(a.LogDir, "aisomnia.log") }

func (a Agent) agentLogPath() string { return filepath.Join(a.LogDir, "aisomnia-agent.log") }

// RenderPlist fills the launchd template; profilePath "" means the built-in profile.
func RenderPlist(exe, profilePath, agentLog string) string {
	profileArgs := ""
	if profilePath != "" {
		profileArgs = "\t\t<string>--profile</string>\n\t\t<string>" + xmlEscape(profilePath) + "</string>\n"
	}
	return strings.NewReplacer("{{EXE}}", xmlEscape(exe), "{{PROFILE_ARGS}}", profileArgs, "{{AGENTLOG}}", xmlEscape(agentLog)).Replace(plistTemplate)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// Start writes the agent's plist for exe and profilePath and (re)loads it.
func (a Agent) Start(exe, profilePath string) error {
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if profilePath != "" {
		if abs, err := filepath.Abs(profilePath); err == nil {
			profilePath = abs
		}
	}
	_ = os.MkdirAll(a.LogDir, 0o755)
	if err := os.MkdirAll(filepath.Dir(a.PlistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(a.PlistPath, []byte(RenderPlist(exe, profilePath, a.agentLogPath())), 0o644); err != nil {
		return err
	}
	// Reload if already loaded so a new binary path or profile takes effect.
	if _, err := a.Runner.Run("launchctl", "print", a.service()); err == nil {
		_, _ = a.Runner.Run("launchctl", "bootout", a.service())
	}
	_, err := a.Runner.Run("launchctl", "bootstrap", a.target(), a.PlistPath)
	return err
}

// Stop unloads the agent and removes its plist.
func (a Agent) Stop() error {
	if _, err := a.Runner.Run("launchctl", "bootout", a.service()); err != nil &&
		!strings.Contains(err.Error(), "No such process") && !strings.Contains(err.Error(), "Could not find") {
		return err
	}
	if err := os.Remove(a.PlistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// State reports what launchd says about the agent.
func (a Agent) State() macos.AgentState {
	return macos.ParseLaunchctlPrint(a.Runner.Run("launchctl", "print", a.service()))
}

// LastLogLine returns the newest line of the watcher log, or "".
func (a Agent) LastLogLine() string {
	b, err := os.ReadFile(a.LogPath())
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}

// Log appends one timestamped line to the watcher log (0600, rotated at 1 MB).
func (a Agent) Log(format string, args ...any) {
	_ = os.MkdirAll(a.LogDir, 0o755)
	if st, err := os.Stat(a.LogPath()); err == nil && st.Size() > logMaxBytes {
		_ = os.Rename(a.LogPath(), a.LogPath()+".1")
	}
	f, err := os.OpenFile(a.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

// Run is the daemon loop launchd starts: every poll it reads the clamshell sensor and sleeps the
// Mac when Watcher says so. lidHold reports an active lid hold. It returns on SIGINT or SIGTERM.
func (a Agent) Run(poll time.Duration, lidHold func() bool, _ io.Writer) error {
	w := &Watcher{Poll: poll}
	a.Log("lidwatch start (poll %s, pid %d)", poll, os.Getpid())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		select {
		case s := <-sig:
			a.Log("lidwatch stop (%s)", s)
			return nil
		case <-tick.C:
		}
		out, err := a.Runner.Run("ioreg", "-r", "-k", "AppleClamshellState", "-d", "4")
		if err != nil {
			a.Log("ioreg: %v", err)
			continue
		}
		closed, ok := macos.ParseClamshell(out)
		if !ok {
			continue
		}
		if sleep, reason := w.Step(closed, time.Now(), lidHold()); sleep {
			if _, err := a.Runner.Run("pmset", "sleepnow"); err != nil {
				a.Log("sleepnow failed: %v", err)
			} else {
				a.Log("sleep: %s", reason)
			}
		}
	}
}
