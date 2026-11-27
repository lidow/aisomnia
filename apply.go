package main

import (
	"fmt"
)

// cmdApply runs `apply` (profile) or `reset` (defaults profile): only drifted settings produce
// commands, every command is printed before it runs, sudo is asked once.
func (a *app) cmdApply(args []string, p *Profile, verb string) error {
	dryRun := false
	for _, arg := range args {
		switch arg {
		case "--dry-run", "-n":
			dryRun = true
		default:
			return exitf(2, "%s: unknown option %s", verb, arg)
		}
	}
	live, err := readLive(a.runner, currentUID())
	if err != nil {
		return err
	}
	active, _ := a.cleanHolds()
	lidHold := hasHold(active, "lid")
	if verb == "reset" && lidHold {
		return exitf(1, "a lid hold is active; run `aisomnia release` first")
	}
	rows, cmds := assess(p, live, lidHold)
	if len(cmds) == 0 {
		fmt.Fprintf(a.stdout, "%s: nothing to do, %d settings match %q\n", verb, len(rows), p.Name)
		return nil
	}
	if verb == "apply" {
		a.clearHibernateMarker(cmds)
	}
	fmt.Fprintf(a.stdout, "%s: %d command(s) for profile %q\n", verb, len(cmds), p.Name)
	return a.execute(cmds, dryRun)
}

// execute prints and runs the commands in order. One `sudo -v` up front; root commands then run
// with `sudo -n`, so a declined prompt stops everything before the first change.
func (a *app) execute(cmds []Command, dryRun bool) error {
	needSudo := false
	for _, c := range cmds {
		fmt.Fprintf(a.stdout, "  $ %s\n", c)
		if c.Sudo {
			needSudo = true
		}
	}
	if dryRun {
		fmt.Fprintln(a.stdout, "dry run: nothing changed")
		return nil
	}
	if needSudo {
		fmt.Fprintln(a.stdout, "sudo: your password is asked once for the pmset and plist commands")
		if err := a.runner.RunInteractive("sudo", "-v"); err != nil {
			return exitf(1, "sudo declined; nothing changed")
		}
	}
	hints := map[string]bool{}
	var hintOrder []string
	for i, c := range cmds {
		fmt.Fprintf(a.stdout, "[%d/%d] %s\n", i+1, len(cmds), c)
		var err error
		switch {
		case c.Internal != nil:
			err = c.Internal(a)
		case c.Interactive:
			err = a.runner.RunInteractive(c.Argv[0], c.Argv[1:]...)
		case c.Sudo:
			_, err = a.runner.Run("sudo", append([]string{"-n"}, c.Argv...)...)
		default:
			_, err = a.runner.Run(c.Argv[0], c.Argv[1:]...)
		}
		if err != nil {
			return exitf(1, "%s failed: %v (stopped; %d of %d done)", c.Setting, err, i, len(cmds))
		}
		if c.Hint != "" && !hints[c.Hint] {
			hints[c.Hint] = true
			hintOrder = append(hintOrder, c.Hint)
		}
	}
	fmt.Fprintf(a.stdout, "done: %d command(s)\n", len(cmds))
	for _, h := range hintOrder {
		fmt.Fprintf(a.stdout, "next: %s\n", h)
	}
	return nil
}
