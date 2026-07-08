package main

import (
	"os"

	"github.com/jontuk/multimux/cmd"
)

// version is injected by goreleaser via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cmd.Execute(os.Args[1:], version, os.Stdout, os.Stderr))
}
