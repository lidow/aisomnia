package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// userProfilePath is where `aisomnia profile --init` writes the editable copy.
func userProfilePath(home string) string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appName, "profile.toml")
	}
	return filepath.Join(home, ".config", appName, "profile.toml")
}

// resolveProfilePath picks the profile: --profile, then AISOMNIA_PROFILE, then the user copy when it
// exists, else "" for the built-in profile.
func resolveProfilePath(flag, env, home string) string {
	if flag != "" {
		return flag
	}
	if env != "" {
		return env
	}
	if p := userProfilePath(home); fileExists(p) {
		return p
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func (a *app) cmdProfile(args []string) error {
	init, path, force := false, false, false
	for _, arg := range args {
		switch arg {
		case "--init":
			init = true
		case "--path":
			path = true
		case "--force":
			force = true
		default:
			return exitf(2, "profile: unknown option %s (--init, --path, --force)", arg)
		}
	}
	switch {
	case path:
		if a.profilePath == "" {
			fmt.Fprintln(a.stdout, "(built-in)")
		} else {
			fmt.Fprintln(a.stdout, a.profilePath)
		}
		return nil
	case init:
		target := userProfilePath(a.home)
		if fileExists(target) && !force {
			return exitf(1, "%s exists; pass --force to overwrite it", target)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(profileTOML), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "wrote %s — edit it, then run `aisomnia status` and `aisomnia apply`\n", target)
		return nil
	default:
		fmt.Fprint(a.stdout, a.profile.Source)
		return nil
	}
}
