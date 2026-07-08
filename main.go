package main

import (
	"embed"
	"io/fs"
	"os"

	"github.com/jontuk/multimux/cmd"
)

//go:embed all:web/dist
var embeddedWeb embed.FS

// version is injected by goreleaser via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	webFS, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		panic(err)
	}
	os.Exit(cmd.Execute(os.Args[1:], version, webFS, os.Stdout, os.Stderr))
}
