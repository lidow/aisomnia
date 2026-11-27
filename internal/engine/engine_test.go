package engine_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/lidow/aisomnia/internal/engine"
	"github.com/lidow/aisomnia/internal/macos"
	"github.com/lidow/aisomnia/internal/macos/macostest"
	"github.com/lidow/aisomnia/internal/profile"
)

type nopWatcher struct{ started, stopped int }

func (w *nopWatcher) Start() error { w.started++; return nil }
func (w *nopWatcher) Stop() error  { w.stopped++; return nil }

func baselineLive(t *testing.T) *macos.Live {
	t.Helper()
	b, err := os.ReadFile("../macos/testdata/pmset-custom-baseline.txt")
	if err != nil {
		t.Fatal(err)
	}
	saver := "300"
	return &macos.Live{Pmset: macos.ParsePmsetCustom(string(b)), SaverIdle: &saver, LockDelay: "immediate"}
}

func mustDefault(t *testing.T) *profile.Profile {
	t.Helper()
	p, err := profile.Default()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func strs(cmds []engine.Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.String()
	}
	return out
}

func TestAssessOrderAndUnsupported(t *testing.T) {
	rows, cmds := engine.Assess(mustDefault(t), baselineLive(t), false, &nopWatcher{})
	if !engine.HasDrift(rows) {
		t.Fatal("baseline must drift from the default profile")
	}
	unsupported := false
	for _, r := range rows {
		if r.Setting == "pmset battery lidwake" {
			unsupported = r.State == engine.StateUnsupported
		}
	}
	if !unsupported {
		t.Fatal("lidwake is not listed by pmset here and must be reported unsupported")
	}
	want := []string{
		"sudo pmset -b sleep 0",
		"sudo pmset -b displaysleep 5",
		"sudo pmset -c sleep 0",
		"sudo pmset -c displaysleep 30",
		`sudo defaults write /Library/Preferences/com.apple.PowerManagement.plist "Sleep On Power Button" -int 0`,
		"sudo defaults write /Library/Preferences/com.apple.loginwindow PowerButtonSleepsSystem -bool false",
		"aisomnia lidwatch start",
	}
	if got := strings.Join(strs(cmds), "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", got)
	}
}

func TestAssessSkipsSettingsTheMacDoesNotOffer(t *testing.T) {
	b, err := os.ReadFile("../macos/testdata/pmset-cap-apple-silicon.txt")
	if err != nil {
		t.Fatal(err)
	}
	live := baselineLive(t)
	live.PmsetCap = macos.ParsePmsetCap(string(b))
	rows, cmds := engine.Assess(mustDefault(t), live, false, &nopWatcher{})
	for _, r := range rows {
		if strings.Contains(r.Setting, "lidwake") {
			t.Fatalf("lidwake is not offered on this Mac and must not be listed: %+v", r)
		}
		if r.State == engine.StateUnsupported {
			t.Fatalf("no unsupported rows expected when the capabilities are known: %+v", r)
		}
	}
	if len(cmds) != 7 {
		t.Fatalf("the commands must not change: %v", strs(cmds))
	}
}

func TestAssessNoDrift(t *testing.T) {
	live := baselineLive(t)
	for _, src := range []string{"battery", "ac"} {
		live.Pmset[src]["sleep"] = "0"
	}
	live.Pmset["battery"]["displaysleep"], live.Pmset["ac"]["displaysleep"] = "5", "30"
	zero := "0"
	live.SleepOnPowerButton, live.LoginwindowSleeps = &zero, &zero
	live.Agent = macos.AgentState{Loaded: true, PID: "7"}
	rows, cmds := engine.Assess(mustDefault(t), live, false, &nopWatcher{})
	if engine.HasDrift(rows) || len(cmds) != 0 {
		t.Fatalf("expected no drift: %v", strs(cmds))
	}
}

func TestAssessResetDeletesKeysAndStopsWatcher(t *testing.T) {
	base, err := profile.Baseline()
	if err != nil {
		t.Fatal(err)
	}
	live := baselineLive(t)
	zero := "0"
	live.SleepOnPowerButton = &zero
	live.Agent = macos.AgentState{Loaded: true}
	w := &nopWatcher{}
	_, cmds := engine.Assess(base, live, false, w)
	got := strs(cmds)
	if !strings.Contains(strings.Join(got, "\n"), `sudo defaults delete /Library/Preferences/com.apple.PowerManagement.plist "Sleep On Power Button"`) {
		t.Fatalf("reset must delete the power-button key: %v", got)
	}
	if got[len(got)-1] != "aisomnia lidwatch stop" {
		t.Fatalf("reset must stop the watcher last: %v", got)
	}
	if err := cmds[len(cmds)-1].Internal(); err != nil || w.stopped != 1 {
		t.Fatal("the internal stop step must call the watcher")
	}
}

func TestAssessLeftoverDisablesleep(t *testing.T) {
	live := baselineLive(t)
	live.Pmset["battery"]["SleepDisabled"], live.Pmset["ac"]["SleepDisabled"] = "1", "1"
	_, cmds := engine.Assess(mustDefault(t), live, false, &nopWatcher{})
	if !strings.Contains(strings.Join(strs(cmds), "\n"), "sudo pmset -a disablesleep 0") {
		t.Fatal("a leftover SleepDisabled must be reverted")
	}
	_, cmds = engine.Assess(mustDefault(t), live, true, &nopWatcher{})
	if strings.Contains(strings.Join(strs(cmds), "\n"), "disablesleep") {
		t.Fatal("during a lid hold SleepDisabled 1 is expected")
	}
}

func TestBatteryWarning(t *testing.T) {
	live := baselineLive(t)
	if engine.BatteryWarning(mustDefault(t), live) != "" {
		t.Fatal("no warning on AC")
	}
	live.OnBattery, live.BatteryPercent = true, 42
	if !strings.Contains(engine.BatteryWarning(mustDefault(t), live), "42%") {
		t.Fatal("warning on battery")
	}
}

func TestExecuteDryRunRunsNothing(t *testing.T) {
	f := macostest.New()
	var out bytes.Buffer
	cmds := []engine.Command{{Argv: []string{"pmset", "-b", "sleep", "0"}, Sudo: true, Setting: "x"}}
	if err := engine.Execute(f, cmds, true, &out); err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != 0 || !strings.Contains(out.String(), "$ sudo pmset -b sleep 0") {
		t.Fatalf("calls %v, out %s", f.Calls, out.String())
	}
}

func TestExecuteSudoOnceStopsOnDeclineAndFailure(t *testing.T) {
	cmds := []engine.Command{
		{Argv: []string{"pmset", "-b", "sleep", "0"}, Sudo: true, Setting: "a"},
		{Argv: []string{"pmset", "-c", "sleep", "0"}, Sudo: true, Setting: "b"},
		{Argv: []string{"defaults", "-currentHost", "write", "x", "y", "-int", "1"}, Setting: "c"},
	}
	f := macostest.New()
	if err := engine.Execute(f, cmds, false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := "interactive: sudo -v|sudo -n pmset -b sleep 0|sudo -n pmset -c sleep 0|defaults -currentHost write x y -int 1"
	if strings.Join(f.Calls, "|") != want {
		t.Fatalf("calls %v", f.Calls)
	}

	declined := macostest.New()
	declined.Fail["sudo -v"] = "declined"
	if err := engine.Execute(declined, cmds, false, &bytes.Buffer{}); !errors.Is(err, engine.ErrSudoDeclined) || len(declined.Calls) != 1 {
		t.Fatalf("decline: %v %v", err, declined.Calls)
	}

	failing := macostest.New()
	failing.Fail["sudo -n pmset -c sleep 0"] = "boom"
	var se *engine.StepError
	if err := engine.Execute(failing, cmds, false, &bytes.Buffer{}); !errors.As(err, &se) || se.Done != 1 || se.Setting != "b" {
		t.Fatalf("failure: %v", err)
	}
}
