package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jontuk/multimux/internal/pki"
)

func dataDir() string {
	if d := os.Getenv("MULTIMUX_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "multimux")
}

func runCA(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "trust" {
		fmt.Fprintln(stderr, "usage: multimux ca trust")
		return 2
	}
	p := pki.New(filepath.Join(dataDir(), "pki"))
	if _, err := os.Stat(p.CACertPath()); err != nil {
		fmt.Fprintln(stderr, "no CA found — run `multimux serve` once first")
		return 1
	}
	c, err := pki.TrustCommand(runtime.GOOS, p.CACertPath())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	c.Stdout, c.Stderr = stdout, stderr
	if err := c.Run(); err != nil {
		fmt.Fprintf(stderr, "trust install failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "CA installed into OS trust store")
	return 0
}
