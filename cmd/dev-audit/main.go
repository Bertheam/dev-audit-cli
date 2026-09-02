package main

import (
	"fmt"
	"io"
	"os"
)

const version = "0.0.0-lot4"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 1 && arguments[0] == "version" {
		fmt.Fprintln(stdout, version)
		return 0
	}

	fmt.Fprintln(stderr, "Lots 0-4 expose only: dev-audit version; scan arrives in Lot 5")
	return 2
}
