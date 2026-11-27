// Package profile declares the power profile: pmset values per power source, the power-button
// keys, the screen saver and lock, and the lid watcher. Two profiles are embedded: the default
// one aisomnia applies and the macOS baseline that reset restores.
package profile

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

//go:embed profiles/default.toml
var defaultTOML string

//go:embed profiles/baseline.toml
var baselineTOML string

// DefaultTOML is the text of the built-in profile; `aisomnia profile --init` writes it out.
func DefaultTOML() string { return defaultTOML }

// Profile is the declared power profile. Pointer fields in PowerButton are "absent" when nil,
// which means the key must not exist on the Mac (reset deletes it).
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

// Parse reads a profile; unknown keys are an error so a typo cannot silently do nothing.
func Parse(text string) (*Profile, error) {
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

// Default returns the built-in profile.
func Default() (*Profile, error) { return Parse(defaultTOML) }

// Baseline returns the macOS baseline that reset restores.
func Baseline() (*Profile, error) { return Parse(baselineTOML) }

// Load returns the profile at path, or the built-in one when path is "".
func Load(path string) (*Profile, error) {
	if path == "" {
		return Default()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("profile %s: %w", path, err)
	}
	p, err := Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// UserPath is where `aisomnia profile --init` writes the editable copy.
func UserPath(home string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "aisomnia", "profile.toml")
	}
	return filepath.Join(home, ".config", "aisomnia", "profile.toml")
}

// ResolvePath picks the profile: the --profile flag, then AISOMNIA_PROFILE, then the user copy when
// it exists, else "" for the built-in profile.
func ResolvePath(flag, env, home string) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	if p := UserPath(home); fileExists(p) {
		return p
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// pmsetOrder is the order pmset keys are listed and applied; keys not listed follow, sorted.
var pmsetOrder = []string{"sleep", "displaysleep", "disksleep", "tcpkeepalive", "womp", "lidwake", "powernap", "standby", "hibernatemode"}

// OrderedKeys returns the keys of a pmset map in apply order.
func OrderedKeys(m map[string]int) []string {
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
