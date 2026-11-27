package main

import (
	_ "embed"
	"fmt"
	"os"
	"sort"

	"github.com/BurntSushi/toml"
)

//go:embed profile.toml
var profileTOML string

//go:embed profile_defaults.toml
var defaultsTOML string

// Profile is the declared power profile. Pointer fields in PowerButton are "absent" when
// nil, which means the key must not exist on the Mac (reset deletes it).
type Profile struct {
	Name  string `toml:"name"`
	Pmset struct {
		Battery map[string]int `toml:"battery"`
		AC      map[string]int `toml:"ac"`
	} `toml:"pmset"`
	PowerButton struct {
		SleepOnPowerButton      *int  `toml:"sleep_on_power_button"`
		LoginwindowSleepsSystem *bool `toml:"loginwindow_sleeps_system"`
	} `toml:"power_button"`
	Screen struct {
		SaverIdleSeconds int    `toml:"saver_idle_seconds"`
		LockDelay        string `toml:"lock_delay"`
	} `toml:"screen"`
	Lidwatch struct {
		Enabled             bool `toml:"enabled"`
		PollSeconds         int  `toml:"poll_seconds"`
		BatteryFloorPercent int  `toml:"battery_floor_percent"`
	} `toml:"lidwatch"`

	Source string `toml:"-"` // the TOML text the profile was parsed from
}

func parseProfile(text string) (*Profile, error) {
	var p Profile
	md, err := toml.Decode(text, &p)
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("profile: unknown keys %v", undecoded)
	}
	if p.Lidwatch.PollSeconds <= 0 {
		p.Lidwatch.PollSeconds = 2
	}
	if p.Lidwatch.BatteryFloorPercent <= 0 {
		p.Lidwatch.BatteryFloorPercent = 20
	}
	if p.Screen.LockDelay == "" {
		p.Screen.LockDelay = "immediate"
	}
	if p.Screen.SaverIdleSeconds <= 0 {
		return nil, fmt.Errorf("profile: screen.saver_idle_seconds must be positive")
	}
	p.Source = text
	return &p, nil
}

// loadProfile returns the embedded profile, or the one at path when given.
func loadProfile(path string) (*Profile, error) {
	if path == "" {
		return parseProfile(profileTOML)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile %s: %w", path, err)
	}
	p, err := parseProfile(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// pmsetOrder is the order pmset keys are listed and applied; keys not listed follow, sorted.
var pmsetOrder = []string{"sleep", "displaysleep", "disksleep", "tcpkeepalive", "womp", "lidwake", "powernap", "standby", "hibernatemode"}

func orderedKeys(m map[string]int) []string {
	seen := map[string]bool{}
	var keys []string
	for _, k := range pmsetOrder {
		if _, ok := m[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}
