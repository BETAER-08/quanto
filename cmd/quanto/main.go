package main

import (
	"os"

	"github.com/BETAER-08/quanto/internal/cli"
)

var version = "dev"

func main() {
	cli.Version = version
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
