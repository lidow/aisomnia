package macos

import (
	"regexp"
	"strconv"
	"strings"
)

// ParsePmsetCustom turns `pmset -g custom` into source → key → value. Sources are "battery" and
// "ac". Keys with spaces ("Sleep On Power Button") keep their spaces.
func ParsePmsetCustom(out string) map[string]map[string]string {
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
		cur[strings.Join(fields[:len(fields)-1], " ")] = fields[len(fields)-1]
	}
	return res
}

// ParsePmsetCap reads `pmset -g cap`: the settings this Mac accepts. It returns nil when the output
// lists none, so callers can fall back to what `pmset -g custom` shows.
func ParsePmsetCap(out string) map[string]bool {
	keys := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasSuffix(t, ":") || strings.Contains(t, " ") {
			continue
		}
		keys[t] = true
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// ParseDefaults interprets a `defaults read` result: (value, set). A missing key is ("", false).
func ParseDefaults(out string, err error) (string, bool) {
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

var screenLockRE = regexp.MustCompile(`screenLock delay is\s+(.+?)\s*$`)

// ParseScreenLock normalizes `sysadminctl -screenLock status`: "immediate", "off" or seconds.
func ParseScreenLock(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if m := screenLockRE.FindStringSubmatch(line); m != nil {
			v := strings.ToLower(strings.TrimSpace(m[1]))
			v = strings.TrimSuffix(v, " seconds")
			return strings.TrimSuffix(v, " second")
		}
	}
	return "unknown"
}

var clamshellRE = regexp.MustCompile(`"AppleClamshellState"\s*=\s*(Yes|No)`)

// ParseClamshell reads `ioreg -r -k AppleClamshellState -d 4`: closed, known.
func ParseClamshell(out string) (closed bool, ok bool) {
	m := clamshellRE.FindStringSubmatch(out)
	if m == nil {
		return false, false
	}
	return m[1] == "Yes", true
}

var batteryPctRE = regexp.MustCompile(`(\d+)%`)

// ParseBattery reads `pmset -g batt`: on battery, percent (-1 when absent).
func ParseBattery(out string) (onBattery bool, percent int) {
	onBattery = strings.Contains(out, "'Battery Power'")
	percent = -1
	if m := batteryPctRE.FindStringSubmatch(out); m != nil {
		percent, _ = strconv.Atoi(m[1])
	}
	return onBattery, percent
}

// AgentState is what `launchctl print` says about a launchd agent.
type AgentState struct {
	Loaded bool
	PID    string
	State  string
}

var (
	launchdPidRE   = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)
	launchdStateRE = regexp.MustCompile(`(?m)^\s*state = (\S+)`)
)

// ParseLaunchctlPrint reads `launchctl print gui/<uid>/<label>`; an error means not loaded.
func ParseLaunchctlPrint(out string, err error) AgentState {
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
