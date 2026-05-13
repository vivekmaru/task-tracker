package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/vivek/agent-task-tracker/internal/config"
)

type command struct {
	name        string
	description string
}

var commands = []command{
	{"server", "Start the Forge API server."},
	{"worker", "Run Forge background workers."},
	{"mcp", "Start the Forge MCP server."},
	{"tui", "Open the Forge terminal UI."},
	{"create", "Create a ticket."},
	{"propose", "Propose agent-discovered work."},
	{"claim-next", "Atomically claim the next eligible ticket."},
	{"heartbeat", "Extend an attempt lease."},
	{"checkpoint", "Record resumable attempt progress."},
	{"complete", "Complete an attempt."},
	{"fail", "Fail an attempt."},
	{"block", "Mark an attempt blocked."},
	{"attach", "Attach or register proof artifacts."},
	{"list", "List tickets."},
	{"get", "Show a ticket or attempt."},
	{"codex", "Codex harness convenience commands."},
}

// Run executes the Forge CLI and returns a process-style exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return 0
	}

	name := args[0]
	if !isKnownCommand(name) {
		fmt.Fprintf(stderr, "unknown command %q\n\n", name)
		printHelp(stderr)
		return 2
	}

	if len(args) > 1 && (args[1] == "help" || args[1] == "--help" || args[1] == "-h") {
		printCommandHelp(stdout, name)
		return 0
	}

	if name == "server" || name == "worker" {
		return runProcess(name, args[1:], stdout, stderr)
	}

	fmt.Fprintf(stderr, "command %q is not implemented yet\n", name)
	return 1
}

func runProcess(name string, args []string, stdout, stderr io.Writer) int {
	opts, ok := parseProcessOptions(args, stderr)
	if !ok {
		return 2
	}

	cfg, err := config.Load(opts)
	if err != nil {
		fmt.Fprintf(stderr, "%s configuration error: %v\n", name, err)
		return 2
	}

	switch name {
	case "server":
		if err := cfg.ValidateServer(); err != nil {
			fmt.Fprintf(stderr, "server configuration error: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, "server startup configuration ok; server runtime not implemented yet")
	case "worker":
		if err := cfg.ValidateWorker(); err != nil {
			fmt.Fprintf(stderr, "worker configuration error: %v\n", err)
			return 2
		}
		fmt.Fprintln(stdout, "worker startup configuration ok; worker runtime not implemented yet")
	}
	return 0
}

func parseProcessOptions(args []string, stderr io.Writer) (config.Options, bool) {
	var opts config.Options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--config requires a path")
				return config.Options{}, false
			}
			i++
			opts.ConfigPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			opts.ConfigPath = strings.TrimPrefix(arg, "--config=")
			if opts.ConfigPath == "" {
				fmt.Fprintln(stderr, "--config requires a path")
				return config.Options{}, false
			}
		default:
			fmt.Fprintf(stderr, "unknown flag %q\n", arg)
			return config.Options{}, false
		}
	}
	return opts, true
}

func isKnownCommand(name string) bool {
	for _, cmd := range commands {
		if cmd.name == name {
			return true
		}
	}
	return false
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Forge")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "A pull-based work ledger for autonomous AI agents.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  forge <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, cmd := range commands {
		fmt.Fprintf(w, "  %-12s %s\n", cmd.name, cmd.description)
	}
}

func printCommandHelp(w io.Writer, name string) {
	for _, cmd := range commands {
		if cmd.name == name {
			fmt.Fprintf(w, "Usage:\n  forge %s [flags]\n\n%s\n", cmd.name, strings.TrimSuffix(cmd.description, "."))
			return
		}
	}
}
