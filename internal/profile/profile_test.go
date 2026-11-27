package profile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lidow/aisomnia/internal/profile"
)

func TestEmbeddedProfiles(t *testing.T) {
	p, err := profile.Default()
	if err != nil {
		t.Fatal(err)
	}
	if p.Pmset.Battery["sleep"] != 0 || p.Pmset.AC["displaysleep"] != 30 || p.Pmset.Battery["displaysleep"] != 5 {
		t.Fatalf("pmset: %+v", p.Pmset)
	}
	if p.Screen.SaverIdleSeconds != 300 || p.Screen.LockDelay != "immediate" {
		t.Fatalf("screen: %+v", p.Screen)
	}
	if p.PowerButton.SleepOnPowerButton == nil || *p.PowerButton.SleepOnPowerButton != 0 {
		t.Fatal("sleep_on_power_button must be 0")
	}
	b, err := profile.Baseline()
	if err != nil {
		t.Fatal(err)
	}
	if b.PowerButton.SleepOnPowerButton != nil || b.Lidwatch.Enabled {
		t.Fatalf("baseline: %+v", b)
	}
	if !strings.Contains(profile.DefaultTOML(), "[pmset.battery]") {
		t.Fatal("DefaultTOML")
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	if _, err := profile.Parse("[screen]\nsaver_idle_seconds = 300\n[typo]\nx = 1\n"); err == nil {
		t.Fatal("unknown keys must be rejected")
	}
	if _, err := profile.Parse("[screen]\nsaver_idle_seconds = 0\n"); err == nil {
		t.Fatal("non-positive saver idle must be rejected")
	}
}

func TestResolvePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := profile.ResolvePath("", "", home); got != "" {
		t.Fatalf("built-in expected, got %q", got)
	}
	if got := profile.ResolvePath("/x.toml", "/y.toml", home); got != "/x.toml" {
		t.Fatalf("flag wins: %q", got)
	}
	if got := profile.ResolvePath("", "/y.toml", home); got != "/y.toml" {
		t.Fatalf("env wins over the user copy: %q", got)
	}
	user := profile.UserPath(home)
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte(profile.DefaultTOML()), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := profile.ResolvePath("", "", home); got != user {
		t.Fatalf("user copy expected: %q", got)
	}
	if _, err := profile.Load(user); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Load(filepath.Join(home, "missing.toml")); err == nil {
		t.Fatal("missing file must fail")
	}
}

func TestOrderedKeys(t *testing.T) {
	got := strings.Join(profile.OrderedKeys(map[string]int{"zeta": 1, "womp": 1, "sleep": 0, "alpha": 2}), ",")
	if got != "sleep,womp,alpha,zeta" {
		t.Fatal(got)
	}
}
