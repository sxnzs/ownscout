package main

import (
	"os"

	"ownscout/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout))
}
