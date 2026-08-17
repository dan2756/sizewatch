// Command sizewatch tracks folder growth and runs a hook on spikes.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// version is set at build time with -ldflags "-X main.version=v1.2.3";
// otherwise the module version from go install is used.
var version = ""

const usage = `sizewatch - tracks folder growth and runs a hook on spikes

Usage:
  sizewatch watch PATH... [flags]     sample every --interval, alert on growth
  sizewatch once PATH... [flags]      take one sample and compare with the last
  sizewatch report [PATH...] [flags]  show sizes, recent change and a trend line
  sizewatch du PATH... [flags]        print the disk usage of each path

Run 'sizewatch COMMAND -h' for the flags of a command.

Flags:
  -h, --help     show this help
  --version      print the version
`

// errReported means the command already printed its errors; exit 1 quietly.
var errReported = errors.New("errors reported")

type app struct {
	stdout, stderr io.Writer
	now            func() time.Time
}

type command func(a *app, ctx context.Context, args []string) error

var commands = map[string]command{
	"watch":  (*app).watch,
	"once":   (*app).once,
	"report": (*app).report,
	"du":     (*app).du,
}

func main() {
	a := &app{stdout: os.Stdout, stderr: os.Stderr, now: time.Now}
	os.Exit(a.run(context.Background(), os.Args[1:]))
}

func (a *app) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Fprint(a.stdout, usage)
		return exitOK
	case "-version", "--version", "version":
		fmt.Fprintln(a.stdout, "sizewatch", buildVersion())
		return exitOK
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(a.stderr, "sizewatch: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}

	err := cmd(a, ctx, args[1:])
	var uerr usageError
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return exitOK
	case errors.As(err, &uerr):
		fmt.Fprintf(a.stderr, "sizewatch %s\nRun 'sizewatch %s -h' for usage.\n", uerr, uerr.cmd)
		return exitUsage
	case errors.Is(err, errReported):
		return exitError
	default:
		fmt.Fprintf(a.stderr, "sizewatch: %v\n", err)
		return exitError
	}
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// errorf prints a non-fatal error for one path.
func (a *app) errorf(format string, args ...any) {
	fmt.Fprintf(a.stderr, "sizewatch: "+format+"\n", args...)
}
