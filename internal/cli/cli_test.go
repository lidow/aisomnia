package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lidow/aisomnia/internal/macos"
	"github.com/lidow/aisomnia/internal/macos/macostest"
	"github.com/lidow/aisomnia/internal/profile"
)

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{"2h": 2 * time.Hour, "90m": 90 * time.Minute, "1h30m": 90 * time.Minute, "45": 45 * time.Minute} {
		if d, err := parseDuration(in); err != nil || d != want {
			t.Errorf("%q: %v %v", in, d, err)
		}
	}
	for _, bad := range []string{"0", "-5m", "soon", ""} {
		if _, err := parseDuration(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestUsageVersionAndErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AISOMNIA_PROFILE", "")
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, &out, &errb, "v9.9.9", macostest.New()); code != 0 || !strings.Contains(out.String(), "aisomnia v9.9.9") {
		t.Fatalf("version: %d %s", code, out.String())
	}
	if code := run([]string{"bogus"}, &out, &errb, "dev", macostest.New()); code != 2 {
		t.Fatalf("unknown verb: %d", code)
	}
	if code := run(nil, &out, &errb, "dev", macostest.New()); code != 2 {
		t.Fatalf("no verb: %d", code)
	}
	if code := run([]string{"--profile", "/nonexistent/profile.toml", "status"}, &out, &errb, "dev", macostest.New()); code != 2 {
		t.Fatalf("missing profile: %d", code)
	}
}

func TestEnvironmentVariableSelectsTheProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := home + "/custom.toml"
	if err := os.WriteFile(path, []byte(strings.Replace(profile.DefaultTOML(), `name = "aisomnia default"`, `name = "from env"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AISOMNIA_PROFILE", path)
	var out, errb bytes.Buffer
	if code := run([]string{"profile", "--path"}, &out, &errb, "dev", macostest.New()); code != 0 || strings.TrimSpace(out.String()) != path {
		t.Fatalf("%d %q %s", code, out.String(), errb.String())
	}
}

func TestProfileInitWritesTheUserCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AISOMNIA_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	var out, errb bytes.Buffer
	if code := run([]string{"profile", "--init"}, &out, &errb, "dev", macostest.New()); code != 0 {
		t.Fatalf("%d %s", code, errb.String())
	}
	if _, err := os.Stat(profile.UserPath(home)); err != nil {
		t.Fatal("user profile not written")
	}
	if code := run([]string{"profile", "--init"}, &out, &errb, "dev", macostest.New()); code != 1 {
		t.Fatal("a second --init without --force must fail")
	}
}

func TestApplyDryRunWithTheFakeMac(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AISOMNIA_PROFILE", "")
	b, err := os.ReadFile("../macos/testdata/pmset-custom-baseline.txt")
	if err != nil {
		t.Fatal(err)
	}
	f := macostest.New()
	f.Out["pmset -g custom"] = string(b)
	f.Out["defaults -currentHost read com.apple.screensaver idleTime"] = "300\n"
	f.Out["sysadminctl -screenLock status"] = "screenLock delay is immediate"
	f.Fail["defaults read /Library/Preferences/com.apple.PowerManagement.plist Sleep On Power Button"] = "does not exist"
	f.Fail["defaults read /Library/Preferences/com.apple.loginwindow PowerButtonSleepsSystem"] = "does not exist"
	f.Fail["launchctl print gui/"+macos.CurrentUID()+"/dev.aisomnia.lidwatch"] = "Could not find service"
	var out, errb bytes.Buffer
	if code := run([]string{"apply", "--dry-run"}, &out, &errb, "dev", f); code != 0 {
		t.Fatalf("%d %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "apply: 7 command(s)") || !strings.Contains(out.String(), "dry run: nothing changed") {
		t.Fatal(out.String())
	}
	for _, c := range f.Calls {
		if strings.HasPrefix(c, "sudo") || strings.HasPrefix(c, "interactive") || strings.Contains(c, "bootstrap") {
			t.Fatalf("a dry run must not change anything: %s", c)
		}
	}
	out.Reset()
	if code := run([]string{"status"}, &out, &errb, "dev", f); code != 1 || !strings.Contains(out.String(), "DRIFT") {
		t.Fatalf("status must report drift with exit 1: %d", code)
	}
}
