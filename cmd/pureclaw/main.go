package main

import (
	"fmt"
	"io"
	"os"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 2 {
		printUsage(stderr)
		return 1
	}
	switch args[1] {
	case "version":
		fmt.Fprintln(stdout, Version)
		return 0
	case "init":
		return runInit(stdin, stdout, stderr)
	case "run":
		// Check for --agent flag: pureclaw run --agent <workspace-path> [--config <path>] [--vault <path>]
		agentPath, configPath, vaultPath, err := parseAgentFlags(args[2:])
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if agentPath != "" {
			return runSubAgentCmd(agentPath, configPath, vaultPath, stdin, stderr)
		}
		return runAgent(stdin, stdout, stderr)
	case "vault":
		if len(args) < 3 {
			printVaultUsage(stderr)
			return 1
		}
		return runVault(args[2:], stdin, stdout, stderr)
	default:
		printUsage(stderr)
		return 1
	}
}

// parseAgentFlags parses --agent, --config, --vault from args after "run".
// Returns empty agentPath if --agent is not present.
func parseAgentFlags(args []string) (agentPath, configPath, vaultPath string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--agent":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("--agent requires a workspace path argument")
			}
			agentPath = args[i+1]
			i++
		case "--config":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("--config requires a path argument")
			}
			configPath = args[i+1]
			i++
		case "--vault":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("--vault requires a path argument")
			}
			vaultPath = args[i+1]
			i++
		}
	}
	// Default paths if --agent is present but paths not specified.
	if agentPath != "" {
		if configPath == "" {
			configPath = defaultConfigPath
		}
		if vaultPath == "" {
			vaultPath = defaultVaultPath
		}
	}
	return agentPath, configPath, vaultPath, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: pureclaw <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  init      Initialize a new workspace")
	fmt.Fprintln(w, "  run       Start the agent")
	fmt.Fprintln(w, "  vault     Manage encrypted vault")
	fmt.Fprintln(w, "  version   Print version")
}
