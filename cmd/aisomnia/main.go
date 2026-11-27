// Command aisomnia keeps a Mac awake for long-running work (AI agents, builds, syncs), locked for
// compliance, and asleep when the lid closes. It applies a declared power profile and reports drift.
package main

import (
	"os"

	"github.com/lidow/aisomnia/internal/cli"
)

// version is set by the release build (-ldflags "-X main.version=v1.2.3").
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
