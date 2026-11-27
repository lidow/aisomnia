// Package macostest provides a fake macos.Runner for unit tests. It never runs a command.
package macostest

import (
	"errors"
	"strings"
)

// Fake answers from tables keyed by the full command line and records every call.
type Fake struct {
	Out   map[string]string // command line → output
	Fail  map[string]string // command line → error message (output too)
	Calls []string
}

// New returns an empty Fake.
func New() *Fake { return &Fake{Out: map[string]string{}, Fail: map[string]string{}} }

func key(name string, args []string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

// Run records the call and answers from Fail, then Out.
func (f *Fake) Run(name string, args ...string) (string, error) {
	k := key(name, args)
	f.Calls = append(f.Calls, k)
	if msg, ok := f.Fail[k]; ok {
		return msg, errors.New(msg)
	}
	return f.Out[k], nil
}

// RunInteractive records the call with an "interactive: " prefix.
func (f *Fake) RunInteractive(name string, args ...string) error {
	k := key(name, args)
	f.Calls = append(f.Calls, "interactive: "+k)
	if msg, ok := f.Fail[k]; ok {
		return errors.New(msg)
	}
	return nil
}
