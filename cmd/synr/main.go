package main

import (
	"context"
	"os"

	"github.com/chck/synr/internal/presentation/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, cli.DefaultDependencies()))
}
