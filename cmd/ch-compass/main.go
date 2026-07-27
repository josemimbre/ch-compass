// Command ch-compass is the entrypoint for the CLI.
package main

import (
	"os"

	"github.com/josemimbre/ch-compass/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
