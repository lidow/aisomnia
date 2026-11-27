package engine

import (
	"errors"
	"fmt"
	"io"

	"github.com/lidow/aisomnia/internal/macos"
)

// ErrSudoDeclined means the user declined the sudo prompt; nothing changed.
var ErrSudoDeclined = errors.New("sudo declined; nothing changed")

// StepError reports the command that failed and how many ran before it.
type StepError struct {
	Setting string
	Done    int
	Total   int
	Err     error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("%s failed: %v (stopped; %d of %d done)", e.Setting, e.Err, e.Done, e.Total)
}

func (e *StepError) Unwrap() error { return e.Err }

// Execute prints and runs the commands in order. One `sudo -v` up front; root commands then run
// with `sudo -n`, so a declined prompt stops everything before the first change.
func Execute(r macos.Runner, cmds []Command, dryRun bool, out io.Writer) error {
	needSudo := false
	for _, c := range cmds {
		fmt.Fprintf(out, "  $ %s\n", c)
		if c.Sudo {
			needSudo = true
		}
	}
	if dryRun {
		fmt.Fprintln(out, "dry run: nothing changed")
		return nil
	}
	if needSudo {
		fmt.Fprintln(out, "sudo: your password is asked once for the pmset and plist commands")
		if err := r.RunInteractive("sudo", "-v"); err != nil {
			return ErrSudoDeclined
		}
	}
	seen := map[string]bool{}
	var hints []string
	for i, c := range cmds {
		fmt.Fprintf(out, "[%d/%d] %s\n", i+1, len(cmds), c)
		var err error
		switch {
		case c.Internal != nil:
			err = c.Internal()
		case c.Interactive:
			err = r.RunInteractive(c.Argv[0], c.Argv[1:]...)
		case c.Sudo:
			_, err = r.Run("sudo", append([]string{"-n"}, c.Argv...)...)
		default:
			_, err = r.Run(c.Argv[0], c.Argv[1:]...)
		}
		if err != nil {
			return &StepError{Setting: c.Setting, Done: i, Total: len(cmds), Err: err}
		}
		if c.Hint != "" && !seen[c.Hint] {
			seen[c.Hint] = true
			hints = append(hints, c.Hint)
		}
	}
	fmt.Fprintf(out, "done: %d command(s)\n", len(cmds))
	for _, h := range hints {
		fmt.Fprintf(out, "next: %s\n", h)
	}
	return nil
}
