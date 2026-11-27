package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRunner answers from a table and records calls; it never touches macOS.
type fakeRunner struct {
	answers map[string]string
	fail    map[string]string
	calls   []string
}

func (f *fakeRunner) key(name string, args ...string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	k := f.key(name, args...)
	f.calls = append(f.calls, k)
	if msg, ok := f.fail[k]; ok {
		return msg, errors.New(msg)
	}
	return f.answers[k], nil
}

func (f *fakeRunner) RunInteractive(name string, args ...string) error {
	k := f.key(name, args...)
	f.calls = append(f.calls, "interactive: "+k)
	if msg, ok := f.fail[k]; ok {
		return errors.New(msg)
	}
	return nil
}

const pmsetBaseline = `Battery Power:
 Sleep On Power Button 1
 lowpowermode         0
 standby              1
 hibernatemode        3
 powernap             1
 displaysleep         10
 womp                 0
 sleep                1
 tcpkeepalive         1
 disksleep            10
AC Power:
 Sleep On Power Button 1
 standby              1
 hibernatemode        3
 powernap             1
 displaysleep         10
 womp                 1
 sleep                1
 tcpkeepalive         1
 disksleep            10
`

func baselineRunner() *fakeRunner {
	return &fakeRunner{
		answers: map[string]string{
			"pmset -g custom": pmsetBaseline,
			"defaults -currentHost read com.apple.screensaver idleTime": "300\n",
			"sysadminctl -screenLock status":                            "2026-11-26 14:06:03.423 sysadminctl[4767:3703392] screenLock delay is immediate\n",
			"pmset -g batt":                                             "Now drawing from 'AC Power'\n -InternalBattery-0 (id=36044899)\t100%; charged; 0:00 remaining present: true\n",
		},
		fail: map[string]string{
			"defaults read /Library/Preferences/com.apple.PowerManagement.plist Sleep On Power Button": "The domain/default pair of (/Library/Preferences/com.apple.PowerManagement.plist, Sleep On Power Button) does not exist",
			"defaults read /Library/Preferences/com.apple.loginwindow PowerButtonSleepsSystem":         "Could not find key 'PowerButtonSleepsSystem' in domain 'com.apple.loginwindow'",
			"launchctl print gui/501/" + agentLabel:                                                    "Could not find service",
		},
	}
}

func mustProfile(t *testing.T) *Profile {
	t.Helper()
	p, err := parseProfile(profileTOML)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// ----- parsers -----------------------------------------------------------------

func TestParsePmsetCustom(t *testing.T) {
	m := parsePmsetCustom(pmsetBaseline)
	if m["battery"]["sleep"] != "1" || m["ac"]["womp"] != "1" || m["battery"]["womp"] != "0" {
		t.Fatalf("unexpected parse: %v", m)
	}
	if m["ac"]["Sleep On Power Button"] != "1" {
		t.Fatalf("multi-word key lost: %v", m["ac"])
	}
}

func TestParseScreenLock(t *testing.T) {
	cases := map[string]string{
		"x sysadminctl[1:2] screenLock delay is immediate": "immediate",
		"screenLock delay is 300 seconds":                  "300",
		"screenLock delay is off":                          "off",
		"garbage":                                          "unknown",
	}
	for in, want := range cases {
		if got := parseScreenLock(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestParseClamshellAndBattery(t *testing.T) {
	closed, ok := parseClamshell(`  |   "AppleClamshellState" = Yes`)
	if !ok || !closed {
		t.Fatal("expected closed")
	}
	closed, ok = parseClamshell(`"AppleClamshellState" = No`)
	if !ok || closed {
		t.Fatal("expected open")
	}
	if _, ok := parseClamshell("nothing"); ok {
		t.Fatal("expected unknown")
	}
	onBatt, pct := parseBattery("Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t43%; discharging; 3:10 remaining present: true\n")
	if !onBatt || pct != 43 {
		t.Fatalf("battery: %v %d", onBatt, pct)
	}
}

func TestParseLaunchctlPrint(t *testing.T) {
	st := parseLaunchctlPrint("gui/501/x = {\n\tactive count = 1\n\tpath = /x\n\tstate = running\n\n\tprogram = /x\n\tpid = 4242\n}\n", nil)
	if !st.Loaded || st.PID != "4242" || st.State != "running" {
		t.Fatalf("unexpected: %+v", st)
	}
	if parseLaunchctlPrint("", errors.New("Could not find service")).Loaded {
		t.Fatal("expected not loaded")
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"2h": 2 * time.Hour, "90m": 90 * time.Minute, "1h30m": 90 * time.Minute, "45": 45 * time.Minute} {
		d, err := parseDuration(in)
		if err != nil || d != want {
			t.Errorf("%q: %v %v", in, d, err)
		}
	}
	for _, bad := range []string{"0", "-5m", "soon", ""} {
		if _, err := parseDuration(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

// ----- profile and assess ----------------------------------------------------------

func TestEmbeddedProfilesParse(t *testing.T) {
	p := mustProfile(t)
	if p.Pmset.Battery["sleep"] != 0 || p.Pmset.AC["displaysleep"] != 30 || p.Pmset.Battery["displaysleep"] != 5 {
		t.Fatalf("profile values: %+v", p.Pmset)
	}
	if p.Screen.SaverIdleSeconds != 300 || p.Screen.LockDelay != "immediate" { //  option 1
		t.Fatalf("screen: %+v", p.Screen)
	}
	if p.PowerButton.SleepOnPowerButton == nil || *p.PowerButton.SleepOnPowerButton != 0 {
		t.Fatal("sleep_on_power_button must be 0")
	}
	d, err := parseProfile(defaultsTOML)
	if err != nil {
		t.Fatal(err)
	}
	if d.PowerButton.SleepOnPowerButton != nil || d.Lidwatch.Enabled {
		t.Fatalf("defaults: %+v", d)
	}
	if _, err := parseProfile("[pmset.battery]\nsleep = 0\n[typo]\nx = 1\n"); err == nil {
		t.Fatal("unknown keys must be rejected")
	}
}

func TestAssessOrderAndDrift(t *testing.T) {
	p := mustProfile(t)
	live, err := readLive(baselineRunner(), "501")
	if err != nil {
		t.Fatal(err)
	}
	rows, cmds := assess(p, live, false)
	if !hasDrift(rows) {
		t.Fatal("baseline must drift from the profile")
	}
	// lidwake is not listed by this Mac → unsupported, no command.
	var sawUnsupported bool
	for _, r := range rows {
		if r.Setting == "pmset battery lidwake" {
			sawUnsupported = r.State == stateUnsupported
		}
	}
	if !sawUnsupported {
		t.Fatal("lidwake must be reported unsupported")
	}
	// Order: pmset (battery then ac) → plists → screen saver (ok here) → lock (ok) → lidwatch start.
	var order []string
	for _, c := range cmds {
		order = append(order, c.String())
	}
	joined := strings.Join(order, "\n")
	want := []string{
		"sudo pmset -b sleep 0",
		"sudo pmset -b displaysleep 5",
		"sudo pmset -c sleep 0",
		"sudo pmset -c displaysleep 30",
		`sudo defaults write /Library/Preferences/com.apple.PowerManagement.plist "Sleep On Power Button" -int 0`,
		"sudo defaults write /Library/Preferences/com.apple.loginwindow PowerButtonSleepsSystem -bool false",
		"aisomnia lidwatch start",
	}
	if len(order) != len(want) {
		t.Fatalf("got %d commands:\n%s", len(order), joined)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("command %d: got %q want %q\nall:\n%s", i, order[i], want[i], joined)
		}
	}
	for _, c := range cmds {
		if strings.HasPrefix(c.Setting, "pmset") && !c.Sudo {
			t.Fatalf("%s must use sudo", c.Setting)
		}
	}
}

func TestAssessNoDriftProducesNoCommands(t *testing.T) {
	p := mustProfile(t)
	r := baselineRunner()
	applied := strings.NewReplacer(" sleep                1", " sleep                0").Replace(pmsetBaseline)
	applied = strings.Replace(applied, " displaysleep         10", " displaysleep         5", 1)
	applied = strings.Replace(applied, " displaysleep         10", " displaysleep         30", 1)
	r.answers["pmset -g custom"] = applied
	delete(r.fail, "defaults read /Library/Preferences/com.apple.PowerManagement.plist Sleep On Power Button")
	r.answers["defaults read /Library/Preferences/com.apple.PowerManagement.plist Sleep On Power Button"] = "0\n"
	delete(r.fail, "defaults read /Library/Preferences/com.apple.loginwindow PowerButtonSleepsSystem")
	r.answers["defaults read /Library/Preferences/com.apple.loginwindow PowerButtonSleepsSystem"] = "0\n"
	delete(r.fail, "launchctl print gui/501/"+agentLabel)
	r.answers["launchctl print gui/501/"+agentLabel] = "state = running\npid = 7\n"
	live, _ := readLive(r, "501")
	rows, cmds := assess(p, live, false)
	if hasDrift(rows) || len(cmds) != 0 {
		t.Fatalf("expected no drift; rows=%+v cmds=%d", rows, len(cmds))
	}
}

func TestAssessResetDeletesKeysAndStopsWatcher(t *testing.T) {
	d, _ := parseProfile(defaultsTOML)
	r := baselineRunner()
	delete(r.fail, "defaults read /Library/Preferences/com.apple.PowerManagement.plist Sleep On Power Button")
	r.answers["defaults read /Library/Preferences/com.apple.PowerManagement.plist Sleep On Power Button"] = "0\n"
	delete(r.fail, "launchctl print gui/501/"+agentLabel)
	r.answers["launchctl print gui/501/"+agentLabel] = "state = running\npid = 7\n"
	live, _ := readLive(r, "501")
	_, cmds := assess(d, live, false)
	var got []string
	for _, c := range cmds {
		got = append(got, c.String())
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, `sudo defaults delete /Library/Preferences/com.apple.PowerManagement.plist "Sleep On Power Button"`) {
		t.Fatalf("reset must delete the power-button key:\n%s", joined)
	}
	if got[len(got)-1] != "aisomnia lidwatch stop" {
		t.Fatalf("reset must stop the watcher last:\n%s", joined)
	}
	if strings.Contains(joined, "pmset -b sleep") {
		t.Fatalf("baseline already has sleep 1; no pmset sleep command expected:\n%s", joined)
	}
}

func TestAssessLeftoverDisablesleep(t *testing.T) {
	p := mustProfile(t)
	r := baselineRunner()
	r.answers["pmset -g custom"] = strings.Replace(pmsetBaseline, " sleep                1\n", " sleep                1\n SleepDisabled        1\n", 2)
	live, _ := readLive(r, "501")
	_, cmds := assess(p, live, false)
	found := false
	for _, c := range cmds {
		if c.String() == "sudo pmset -a disablesleep 0" {
			found = true
		}
	}
	if !found {
		t.Fatal("leftover SleepDisabled must be reverted by apply")
	}
	_, cmds = assess(p, live, true)
	for _, c := range cmds {
		if c.String() == "sudo pmset -a disablesleep 0" {
			t.Fatal("during a lid hold SleepDisabled 1 is expected")
		}
	}
}

// ----- apply execution --------------------------------------------------------------

func testApp(r Runner, p *Profile) (*app, *bytes.Buffer) {
	var out bytes.Buffer
	d, _ := parseProfile(defaultsTOML)
	return &app{runner: r, profile: p, defaults: d, home: "/nonexistent", stdout: &out, stderr: &out}, &out
}

func TestExecuteDryRunRunsNothing(t *testing.T) {
	r := baselineRunner()
	a, out := testApp(r, mustProfile(t))
	cmds := []Command{{Argv: []string{"pmset", "-b", "sleep", "0"}, Sudo: true, Setting: "x"}}
	if err := a.execute(cmds, true); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("dry run ran %v", r.calls)
	}
	if !strings.Contains(out.String(), "$ sudo pmset -b sleep 0") {
		t.Fatalf("command not printed: %s", out.String())
	}
}

func TestExecuteSudoOnceAndStopsOnDecline(t *testing.T) {
	r := baselineRunner()
	a, _ := testApp(r, mustProfile(t))
	cmds := []Command{
		{Argv: []string{"pmset", "-b", "sleep", "0"}, Sudo: true, Setting: "a"},
		{Argv: []string{"pmset", "-c", "sleep", "0"}, Sudo: true, Setting: "b"},
		{Argv: []string{"defaults", "-currentHost", "write", "x", "y", "-int", "1"}, Setting: "c"},
	}
	if err := a.execute(cmds, false); err != nil {
		t.Fatal(err)
	}
	want := []string{"interactive: sudo -v", "sudo -n pmset -b sleep 0", "sudo -n pmset -c sleep 0", "defaults -currentHost write x y -int 1"}
	if strings.Join(r.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v", r.calls)
	}
	r2 := baselineRunner()
	r2.fail["sudo -v"] = "declined"
	a2, _ := testApp(r2, mustProfile(t))
	err := a2.execute(cmds, false)
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 1 || len(r2.calls) != 1 {
		t.Fatalf("expected exit 1 after sudo decline with no commands run; err=%v calls=%v", err, r2.calls)
	}
}

// ----- watcher ------------------------------------------------------------------------

func TestWatcherTransitions(t *testing.T) {
	w := &watcher{poll: 2 * time.Second}
	t0 := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	if s, _ := w.step(true, t0, false); s {
		t.Fatal("first observation must not sleep")
	}
	if s, _ := w.step(false, t0.Add(2*time.Second), false); s {
		t.Fatal("open lid must not sleep")
	}
	s, reason := w.step(true, t0.Add(4*time.Second), false)
	if !s || reason != "lid closed" {
		t.Fatalf("open→closed must sleep: %v %q", s, reason)
	}
	if s, _ := w.step(true, t0.Add(6*time.Second), false); s {
		t.Fatal("still closed, no wake: must not sleep again")
	}
	// A long gap between ticks means the Mac slept and woke; lid still closed → sleep again.
	s, reason = w.step(true, t0.Add(10*time.Minute), false)
	if !s || reason != "woke with the lid closed" {
		t.Fatalf("wake while closed: %v %q", s, reason)
	}
	// A lid hold pauses the watcher, and the transition during the hold is not replayed afterwards.
	w2 := &watcher{poll: 2 * time.Second}
	w2.step(false, t0, false)
	if s, _ := w2.step(true, t0.Add(2*time.Second), true); s {
		t.Fatal("lid hold must pause")
	}
	if s, _ := w2.step(true, t0.Add(4*time.Second), false); s {
		t.Fatal("no transition after the hold ends while still closed")
	}
}

// ----- holds ---------------------------------------------------------------------------

func TestSplitHolds(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	holds := []Hold{
		{Kind: "lid", Expires: now.Add(time.Hour), PID: 1},
		{Kind: "display", Expires: now.Add(-time.Minute), PID: 2},
		{Kind: "lock", Expires: now.Add(time.Hour), PID: 3},
	}
	alive := func(pid int) bool { return pid != 3 }
	active, stale := splitHolds(holds, now, alive)
	if len(active) != 1 || active[0].Kind != "lid" {
		t.Fatalf("active: %+v", active)
	}
	if len(stale) != 2 {
		t.Fatalf("stale: %+v", stale)
	}
	if !hasHold(active, "lid") || hasHold(active, "lock") {
		t.Fatal("hasHold")
	}
}

// ----- profile resolution and dispatch ---------------------------------------------------

func TestResolveProfilePath(t *testing.T) {
	home := t.TempDir()
	if got := resolveProfilePath("", "", home); got != "" {
		t.Fatalf("built-in expected, got %q", got)
	}
	if got := resolveProfilePath("/x.toml", "/y.toml", home); got != "/x.toml" {
		t.Fatalf("flag wins: %q", got)
	}
	if got := resolveProfilePath("", "/y.toml", home); got != "/y.toml" {
		t.Fatalf("env wins over user copy: %q", got)
	}
	user := userProfilePath(home)
	writeFile(t, user, profileTOML)
	if got := resolveProfilePath("", "", home); got != user {
		t.Fatalf("user copy expected: %q", got)
	}
}

func TestRunUsageAndVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "aisomnia") {
		t.Fatalf("version: %d %s", code, out.String())
	}
	if code := run([]string{"bogus"}, &out, &errb); code != 2 {
		t.Fatalf("unknown verb: %d", code)
	}
	if code := run([]string{}, &out, &errb); code != 2 {
		t.Fatalf("no verb: %d", code)
	}
	if code := run([]string{"--profile", "/nonexistent/profile.toml", "status"}, &out, &errb); code != 2 {
		t.Fatalf("missing profile: %d", code)
	}
}

func TestProfileInitWritesUserCopy(t *testing.T) {
	home := t.TempDir()
	p, _ := parseProfile(profileTOML)
	var out bytes.Buffer
	a := &app{runner: baselineRunner(), profile: p, home: home, stdout: &out, stderr: &out}
	if err := a.cmdProfile([]string{"--init"}); err != nil {
		t.Fatal(err)
	}
	if !fileExists(userProfilePath(home)) {
		t.Fatal("user profile not written")
	}
	if err := a.cmdProfile([]string{"--init"}); err == nil {
		t.Fatal("second --init without --force must fail")
	}
}
