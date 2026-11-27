package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Runner is the one door to macOS commands: unit tests inject a fake.
type Runner interface {
	// Run executes a command and returns its combined output.
	Run(name string, args ...string) (string, error)
	// RunInteractive attaches the terminal: sudo and sysadminctl prompt you here.
	RunInteractive(name string, args ...string) error
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

func (execRunner) RunInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// ----- parsers ---------------------------------------------------------------

// parsePmsetCustom turns `pmset -g custom` into source → key → value. Sources are "battery" and
// "ac". Keys with spaces ("Sleep On Power Button") keep their spaces.
func parsePmsetCustom(out string) map[string]map[string]string {
	res := map[string]map[string]string{"battery": {}, "ac": {}}
	var cur map[string]string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(line, " ") {
			switch strings.ToLower(trimmed) {
			case "battery power:":
				cur = res["battery"]
			case "ac power:":
				cur = res["ac"]
			default:
				cur = map[string]string{}
			}
			continue
		}
		if cur == nil {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		key := strings.Join(fields[:len(fields)-1], " ")
		cur[key] = fields[len(fields)-1]
	}
	return res
}

// parseDefaults interprets a `defaults read` result: (value, set). A missing key is (“”, false).
func parseDefaults(out string, err error) (string, bool) {
	if err != nil {
		low := strings.ToLower(out + err.Error())
		if strings.Contains(low, "does not exist") || strings.Contains(low, "could not find key") {
			return "", false
		}
		return "", false
	}
	return strings.TrimSpace(out), true
}

var screenLockRE = regexp.MustCompile(`screenLock delay is\s+(.+?)\s*$`)

// parseScreenLock normalizes `sysadminctl -screenLock status`: "immediate", "off" or seconds.
func parseScreenLock(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if m := screenLockRE.FindStringSubmatch(line); m != nil {
			v := strings.ToLower(strings.TrimSpace(m[1]))
			v = strings.TrimSuffix(v, " seconds")
			v = strings.TrimSuffix(v, " second")
			return v
		}
	}
	return "unknown"
}

var clamshellRE = regexp.MustCompile(`"AppleClamshellState"\s*=\s*(Yes|No)`)

// parseClamshell reads `ioreg -r -k AppleClamshellState -d 4`: closed, known.
func parseClamshell(out string) (closed bool, ok bool) {
	m := clamshellRE.FindStringSubmatch(out)
	if m == nil {
		return false, false
	}
	return m[1] == "Yes", true
}

var batteryPctRE = regexp.MustCompile(`(\d+)%`)

// parseBattery reads `pmset -g batt`: on battery, percent (-1 when absent).
func parseBattery(out string) (onBattery bool, percent int) {
	onBattery = strings.Contains(out, "'Battery Power'")
	percent = -1
	if m := batteryPctRE.FindStringSubmatch(out); m != nil {
		percent, _ = strconv.Atoi(m[1])
	}
	return onBattery, percent
}

// AgentState is what `launchctl print` says about the lid watcher.
type AgentState struct {
	Loaded bool
	PID    string
	State  string
}

var (
	launchdPidRE   = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)
	launchdStateRE = regexp.MustCompile(`(?m)^\s*state = (\S+)`)
)

func parseLaunchctlPrint(out string, err error) AgentState {
	if err != nil {
		return AgentState{}
	}
	st := AgentState{Loaded: true}
	if m := launchdPidRE.FindStringSubmatch(out); m != nil {
		st.PID = m[1]
	}
	if m := launchdStateRE.FindStringSubmatch(out); m != nil {
		st.State = m[1]
	}
	return st
}

// ----- live state ------------------------------------------------------------

// Live is everything status compares the profile against.
type Live struct {
	Pmset              map[string]map[string]string
	SleepOnPowerButton *string
	LoginwindowSleeps  *string
	SaverIdle          *string
	LockDelay          string
	Agent              AgentState
	OnBattery          bool
	BatteryPercent     int
}

const (
	pmPlist          = "/Library/Preferences/com.apple.PowerManagement.plist"
	pmPlistKey       = "Sleep On Power Button"
	loginwindowPlist = "/Library/Preferences/com.apple.loginwindow"
	loginwindowKey   = "PowerButtonSleepsSystem"
	screensaverDom   = "com.apple.screensaver"
	screensaverKey   = "idleTime"
)

func readLive(r Runner, uid string) (*Live, error) {
	out, err := r.Run("pmset", "-g", "custom")
	if err != nil {
		return nil, err
	}
	live := &Live{Pmset: parsePmsetCustom(out)}

	if v, ok := parseDefaults(r.Run("defaults", "read", pmPlist, pmPlistKey)); ok {
		live.SleepOnPowerButton = &v
	}
	if v, ok := parseDefaults(r.Run("defaults", "read", loginwindowPlist, loginwindowKey)); ok {
		live.LoginwindowSleeps = &v
	}
	if v, ok := parseDefaults(r.Run("defaults", "-currentHost", "read", screensaverDom, screensaverKey)); ok {
		live.SaverIdle = &v
	}
	out, _ = r.Run("sysadminctl", "-screenLock", "status")
	live.LockDelay = parseScreenLock(out)
	live.Agent = parseLaunchctlPrint(r.Run("launchctl", "print", "gui/"+uid+"/"+agentLabel))
	out, _ = r.Run("pmset", "-g", "batt")
	live.OnBattery, live.BatteryPercent = parseBattery(out)
	return live, nil
}

func currentUID() string { return strconv.Itoa(os.Getuid()) }
