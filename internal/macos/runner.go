// Package macos is the only door to macOS commands (pmset, defaults, sysadminctl, ioreg, launchctl,
// sudo) and holds the parsers for their output. Everything else talks to the Mac through Runner, so
// unit tests can inject macostest.Fake and never touch the machine.
package macos

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Runner runs macOS commands.
type Runner interface {
	// Run executes a command and returns its combined output.
	Run(name string, args ...string) (string, error)
	// RunInteractive attaches the terminal: sudo and sysadminctl prompt the user here.
	RunInteractive(name string, args ...string) error
}

// Exec is the Runner backed by os/exec.
type Exec struct{}

// Run implements Runner.
func (Exec) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// RunInteractive implements Runner.
func (Exec) RunInteractive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// CurrentUID is the user id launchd's gui domain is addressed with.
func CurrentUID() string { return strconv.Itoa(os.Getuid()) }
