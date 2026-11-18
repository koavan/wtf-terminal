// Command wtf-runner runs on a remote Linux machine (a "runner", #41) and
// holds the Claude Code agents wtf starts there. wtf installs it, with
// wtf-mcp (the hook helper) and the status hooks, into
// ~/.local/share/wtf-runner over SSH.
//
// Phase 1 only reports its version; the daemon (agents in tmux, their
// hooks, waiting approvals) and `attach` (wtf's stream over SSH) follow.
package main

import (
	"fmt"
	"os"
)

// version is set at build time (-X main.version=…).
var version = "dev"

// protocol is what wtf and this runner speak; wtf offers an update when
// they differ.
const protocol = 1

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "version":
			fmt.Printf("%s protocol=%d\n", version, protocol)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "usage: wtf-runner --version")
	os.Exit(2)
}
