package power_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lidow/aisomnia/internal/macos/macostest"
	"github.com/lidow/aisomnia/internal/power"
)

func TestLockSleepWake(t *testing.T) {
	f := macostest.New()
	if err := power.Lock(f); err != nil {
		t.Fatal(err)
	}
	if err := power.Sleep(f); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.Local)
	if err := power.WakeAt(f, now.Add(-time.Minute), now); err == nil {
		t.Fatal("a wake in the past must be refused")
	}
	if err := power.WakeAt(f, now.Add(90*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if err := power.WakeIn(f, 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	want := "pmset displaysleepnow|pmset sleepnow|interactive: sudo -v|sudo -n pmset schedule wake 11/27/26 11:30:00|interactive: sudo -v|sudo -n pmset relative wake 7200"
	if strings.Join(f.Calls, "|") != want {
		t.Fatalf("%v", f.Calls)
	}
	declined := macostest.New()
	declined.Fail["sudo -v"] = "no"
	if err := power.WakeIn(declined, time.Hour); !errors.Is(err, power.ErrSudoDeclined) {
		t.Fatal(err)
	}
}

func TestHibernateRestoresAndClearsTheMarker(t *testing.T) {
	power.SetWaitForWake(func(time.Duration) {})
	dir := t.TempDir()
	f := macostest.New()
	if err := power.Hibernate(f, 3, dir, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := "interactive: sudo -v|sudo -n pmset -a hibernatemode 25|pmset sleepnow|sudo -n pmset -a hibernatemode 3"
	if strings.Join(f.Calls, "|") != want {
		t.Fatalf("%v", f.Calls)
	}
	if _, err := os.Stat(power.MarkerPath(dir)); !os.IsNotExist(err) {
		t.Fatal("the marker must be gone after a successful restore")
	}
}
