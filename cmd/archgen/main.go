// Command archgen scaffolds Go modular monoliths built on archkit.
package main

import (
	"os"

	"github.com/werhani-houssemeddine/archgen/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
