package power

import "time"

// SetWaitForWake replaces the wake detector in tests.
func SetWaitForWake(f func(time.Duration)) { waitForWake = f }
