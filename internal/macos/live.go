package macos

// Preference domains and keys the profile manages.
const (
	PowerManagementPlist = "/Library/Preferences/com.apple.PowerManagement.plist"
	SleepOnPowerButton   = "Sleep On Power Button"
	LoginwindowPlist     = "/Library/Preferences/com.apple.loginwindow"
	PowerButtonSleeps    = "PowerButtonSleepsSystem"
	ScreensaverDomain    = "com.apple.screensaver"
	ScreensaverIdleKey   = "idleTime"
)

// Live is everything status compares the profile against.
type Live struct {
	Pmset              map[string]map[string]string
	SleepOnPowerButton *string
	LoginwindowSleeps  *string
	SaverIdle          *string
	LockDelay          string
	Agent              AgentState
	OnBattery          bool
	BatteryPercent     int
}

// ReadLive reads the managed settings from the Mac. agentLabel is the lid watcher's launchd label.
func ReadLive(r Runner, uid, agentLabel string) (*Live, error) {
	out, err := r.Run("pmset", "-g", "custom")
	if err != nil {
		return nil, err
	}
	live := &Live{Pmset: ParsePmsetCustom(out)}
	if v, ok := ParseDefaults(r.Run("defaults", "read", PowerManagementPlist, SleepOnPowerButton)); ok {
		live.SleepOnPowerButton = &v
	}
	if v, ok := ParseDefaults(r.Run("defaults", "read", LoginwindowPlist, PowerButtonSleeps)); ok {
		live.LoginwindowSleeps = &v
	}
	if v, ok := ParseDefaults(r.Run("defaults", "-currentHost", "read", ScreensaverDomain, ScreensaverIdleKey)); ok {
		live.SaverIdle = &v
	}
	out, _ = r.Run("sysadminctl", "-screenLock", "status")
	live.LockDelay = ParseScreenLock(out)
	live.Agent = ParseLaunchctlPrint(r.Run("launchctl", "print", "gui/"+uid+"/"+agentLabel))
	out, _ = r.Run("pmset", "-g", "batt")
	live.OnBattery, live.BatteryPercent = ParseBattery(out)
	return live, nil
}
