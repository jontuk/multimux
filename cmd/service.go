package cmd

import (
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/jontuk/multimux/internal/svc"
)

func runService(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: multimux service install|uninstall|status|logs")
		return 2
	}
	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if err := svc.Install(runtime.GOOS, exe); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "service installed and started — check `multimux service status`")
		return 0
	case "uninstall":
		if err := svc.Uninstall(runtime.GOOS); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "service removed")
		return 0
	case "status":
		out, err := svc.Status(runtime.GOOS)
		fmt.Fprint(stdout, out)
		if err != nil {
			return 1
		}
		return 0
	case "logs":
		cmd, err := svc.LogsCommand(runtime.GOOS)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintln(stderr, "usage: multimux service install|uninstall|status|logs")
		return 2
	}
}
