package macos_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lidow/aisomnia/internal/macos"
	"github.com/lidow/aisomnia/internal/macos/macostest"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParsePmsetCustom(t *testing.T) {
	m := macos.ParsePmsetCustom(read(t, "pmset-custom-baseline.txt"))
	if m["battery"]["sleep"] != "1" || m["ac"]["womp"] != "1" || m["battery"]["womp"] != "0" {
		t.Fatalf("unexpected parse: %v", m)
	}
	if m["ac"]["Sleep On Power Button"] != "1" {
		t.Fatalf("multi-word key lost: %v", m["ac"])
	}
}

func TestParsePmsetCap(t *testing.T) {
	c := macos.ParsePmsetCap(read(t, "pmset-cap-apple-silicon.txt"))
	if !c["displaysleep"] || !c["womp"] || c["lidwake"] {
		t.Fatalf("%v", c)
	}
	if macos.ParsePmsetCap("") != nil {
		t.Fatal("no capabilities listed must mean unknown (nil)")
	}
}

func TestParseScreenLock(t *testing.T) {
	cases := map[string]string{
		read(t, "sysadminctl-screenlock.txt"): "immediate",
		"screenLock delay is 300 seconds":     "300",
		"screenLock delay is off":             "off",
		"garbage":                             "unknown",
	}
	for in, want := range cases {
		if got := macos.ParseScreenLock(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestParseClamshellAndBattery(t *testing.T) {
	if closed, ok := macos.ParseClamshell(read(t, "ioreg-clamshell-closed.txt")); !ok || !closed {
		t.Fatal("expected closed")
	}
	if closed, ok := macos.ParseClamshell(`"AppleClamshellState" = No`); !ok || closed {
		t.Fatal("expected open")
	}
	if _, ok := macos.ParseClamshell("nothing"); ok {
		t.Fatal("expected unknown")
	}
	if onBatt, pct := macos.ParseBattery(read(t, "pmset-batt-battery.txt")); !onBatt || pct != 43 {
		t.Fatalf("battery: %v %d", onBatt, pct)
	}
	if onBatt, pct := macos.ParseBattery(read(t, "pmset-batt-ac.txt")); onBatt || pct != 100 {
		t.Fatalf("ac: %v %d", onBatt, pct)
	}
}

func TestParseLaunchctlPrint(t *testing.T) {
	st := macos.ParseLaunchctlPrint(read(t, "launchctl-print-running.txt"), nil)
	if !st.Loaded || st.PID != "4242" || st.State != "running" {
		t.Fatalf("unexpected: %+v", st)
	}
	if macos.ParseLaunchctlPrint("", errors.New("Could not find service")).Loaded {
		t.Fatal("expected not loaded")
	}
}

func TestReadLive(t *testing.T) {
	f := macostest.New()
	f.Out["pmset -g custom"] = read(t, "pmset-custom-baseline.txt")
	f.Out["defaults -currentHost read com.apple.screensaver idleTime"] = "300\n"
	f.Out["sysadminctl -screenLock status"] = read(t, "sysadminctl-screenlock.txt")
	f.Out["pmset -g batt"] = read(t, "pmset-batt-ac.txt")
	f.Fail["defaults read /Library/Preferences/com.apple.PowerManagement.plist Sleep On Power Button"] = "does not exist"
	f.Fail["defaults read /Library/Preferences/com.apple.loginwindow PowerButtonSleepsSystem"] = "Could not find key"
	f.Fail["launchctl print gui/501/dev.aisomnia.lidwatch"] = "Could not find service"
	live, err := macos.ReadLive(f, "501", "dev.aisomnia.lidwatch")
	if err != nil {
		t.Fatal(err)
	}
	if live.SleepOnPowerButton != nil || *live.SaverIdle != "300" || live.LockDelay != "immediate" || live.Agent.Loaded || live.OnBattery {
		t.Fatalf("%+v", live)
	}
}
