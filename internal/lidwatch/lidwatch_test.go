package lidwatch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lidow/aisomnia/internal/lidwatch"
	"github.com/lidow/aisomnia/internal/macos/macostest"
)

func TestWatcherTransitions(t *testing.T) {
	w := &lidwatch.Watcher{Poll: 2 * time.Second}
	t0 := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	if s, _ := w.Step(true, t0, false); s {
		t.Fatal("the first observation must not sleep")
	}
	if s, _ := w.Step(false, t0.Add(2*time.Second), false); s {
		t.Fatal("an open lid must not sleep")
	}
	if s, reason := w.Step(true, t0.Add(4*time.Second), false); !s || reason != "lid closed" {
		t.Fatalf("open→closed must sleep: %v %q", s, reason)
	}
	if s, _ := w.Step(true, t0.Add(6*time.Second), false); s {
		t.Fatal("still closed, no wake: no second sleep")
	}
	if s, reason := w.Step(true, t0.Add(10*time.Minute), false); !s || reason != "woke with the lid closed" {
		t.Fatalf("a wake while closed must sleep again: %v %q", s, reason)
	}
	w2 := &lidwatch.Watcher{Poll: 2 * time.Second}
	w2.Step(false, t0, false)
	if s, _ := w2.Step(true, t0.Add(2*time.Second), true); s {
		t.Fatal("a lid hold pauses the watcher")
	}
	if s, _ := w2.Step(true, t0.Add(4*time.Second), false); s {
		t.Fatal("the transition during the hold is not replayed")
	}
}

func TestRenderPlist(t *testing.T) {
	p := lidwatch.RenderPlist("/opt/homebrew/bin/aisomnia", "/Users/x/a&b.toml", "/Users/x/Library/Logs/aisomnia/aisomnia-agent.log")
	for _, want := range []string{"<string>dev.aisomnia.lidwatch</string>", "<string>/opt/homebrew/bin/aisomnia</string>",
		"<string>--profile</string>", "a&amp;b.toml", "<string>lidwatch</string>", "<string>run</string>"} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(lidwatch.RenderPlist("/bin/a", "", "/l"), "--profile") {
		t.Error("the built-in profile needs no --profile argument")
	}
}

func TestStartAndStop(t *testing.T) {
	dir := t.TempDir()
	f := macostest.New()
	f.Fail["launchctl print gui/501/dev.aisomnia.lidwatch"] = "Could not find service"
	a := lidwatch.Agent{Runner: f, UID: "501", PlistPath: filepath.Join(dir, "LaunchAgents", lidwatch.Label+".plist"), LogDir: filepath.Join(dir, "Logs")}
	if err := a.Start("/bin/aisomnia", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.PlistPath); err != nil {
		t.Fatal("plist not written")
	}
	if !strings.Contains(strings.Join(f.Calls, "|"), "launchctl bootstrap gui/501 "+a.PlistPath) {
		t.Fatalf("%v", f.Calls)
	}
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.PlistPath); !os.IsNotExist(err) {
		t.Fatal("plist must be removed")
	}
	a.Log("hello %d", 1)
	if !strings.HasSuffix(a.LastLogLine(), "hello 1") {
		t.Fatal(a.LastLogLine())
	}
	if st, _ := os.Stat(a.LogPath()); st.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v", st.Mode().Perm())
	}
}
