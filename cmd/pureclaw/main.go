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

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: pureclaw <command>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  init      Initialize a new workspace")
	fmt.Fprintln(w, "  run       Start the agent")
	fmt.Fprintln(w, "  vault     Manage encrypted vault")
	fmt.Fprintln(w, "  version   Print version")
}
