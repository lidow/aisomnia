package hold_test

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lidow/aisomnia/internal/hold"
	"github.com/lidow/aisomnia/internal/macos/macostest"
)

func TestSplitAndHas(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	holds := []hold.Hold{
		{Kind: hold.Lid, Expires: now.Add(time.Hour), PID: 1},
		{Kind: hold.Display, Expires: now.Add(-time.Minute), PID: 2},
		{Kind: hold.Lock, Expires: now.Add(time.Hour), PID: 3},
	}
	active, stale := hold.Split(holds, now, func(pid int) bool { return pid != 3 })
	if len(active) != 1 || active[0].Kind != hold.Lid || len(stale) != 2 {
		t.Fatalf("active %+v stale %+v", active, stale)
	}
	if !hold.Has(active, hold.Lid) || hold.Has(active, hold.Lock) {
		t.Fatal("Has")
	}
}

func TestStoreRoundTripAndClean(t *testing.T) {
	s := hold.Store{Dir: t.TempDir()}
	now := time.Now()
	if _, err := s.Write(hold.Hold{Kind: hold.Lid, Started: now, Expires: now.Add(time.Hour), PID: 11}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(hold.Hold{Kind: hold.Display, Started: now.Add(time.Second), Expires: now.Add(-time.Second), PID: 12}); err != nil {
		t.Fatal(err)
	}
	if got := s.Read(); len(got) != 2 || got[0].Kind != hold.Lid {
		t.Fatalf("%+v", got)
	}
	active, removed := s.Clean(now, func(int) bool { return true })
	if len(active) != 1 || len(removed) != 1 {
		t.Fatalf("active %d removed %d", len(active), len(removed))
	}
	if _, err := os.Stat(removed[0].File); !os.IsNotExist(err) {
		t.Fatal("the stale file must be removed")
	}
}

func TestPlanFor(t *testing.T) {
	p, err := hold.PlanFor(hold.Lid, 90*time.Minute, "/bin/aisomnia", 20)
	if err != nil || strings.Join(p.Argv, " ") != "sudo /bin/aisomnia _hold-run lid 5400 20" || !p.Sudo {
		t.Fatalf("%+v %v", p, err)
	}
	p, _ = hold.PlanFor(hold.Lock, time.Hour, "", 20)
	if strings.Join(p.Argv, " ") != "caffeinate -u -d -t 3600" || p.Sudo {
		t.Fatalf("%+v", p)
	}
	if _, err := hold.PlanFor("nap", time.Hour, "", 20); err == nil {
		t.Fatal("unknown kind")
	}
}

func TestRootHelperRevertsAtBatteryFloor(t *testing.T) {
	hold.Tick = 10 * time.Millisecond
	f := macostest.New()
	f.Out["pmset -g batt"] = "Now drawing from 'Battery Power'\n -InternalBattery-0\t19%; discharging\n"
	var out bytes.Buffer
	if err := hold.RootHelper(f, hold.Lid, 3600, 20, 1, &out, make(chan os.Signal)); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(f.Calls, "|")
	if !strings.HasPrefix(calls, "pmset -a disablesleep 1|") || !strings.HasSuffix(calls, "|pmset -a disablesleep 0") {
		t.Fatalf("set then revert expected: %s", calls)
	}
	if !strings.Contains(out.String(), "battery at 19% (floor 20%)") {
		t.Fatal(out.String())
	}
}

func TestRootHelperRevertsOnSignal(t *testing.T) {
	hold.Tick = 10 * time.Millisecond
	f := macostest.New()
	stop := make(chan os.Signal, 1)
	stop <- os.Interrupt
	if err := hold.RootHelper(f, hold.Standby, 3600, 20, 1, &bytes.Buffer{}, stop); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.Calls, "|") != "pmset -a standby 0|pmset -a standby 1" {
		t.Fatalf("%v", f.Calls)
	}
}
