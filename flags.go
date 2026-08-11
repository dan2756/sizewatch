package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/walk"
)

// usageError is a bad command line; it exits with status 2.
type usageError struct {
	cmd, msg string
}

func (e usageError) Error() string { return e.cmd + ": " + e.msg }

func usagef(cmd, format string, args ...any) error {
	return usageError{cmd, fmt.Sprintf(format, args...)}
}

// newFlagSet returns a flag set for a subcommand. Parse errors are returned
// rather than printed, and -h prints the usage to stdout (see parseArgs).
func newFlagSet(name, synopsis, about string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {
		w := fs.Output()
		fmt.Fprintf(w, "Usage: sizewatch %s %s\n\n%s\n\nFlags:\n", name, synopsis, about)
		printFlags(w, fs)
	}
	return fs
}

// parseArgs parses args with fs, allowing flags before, between and after
// positional arguments, and returns the positional ones.
func (a *app) parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	pos, err := parseInterspersed(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fs.SetOutput(a.stdout)
		fs.Usage()
		return nil, flag.ErrHelp
	}
	if err != nil {
		return nil, usageError{fs.Name(), err.Error()}
	}
	return pos, nil
}

// parseInterspersed works around the flag package stopping at the first
// positional argument. Everything after "--" is positional.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		if n := len(args) - len(rest); n > 0 && args[n-1] == "--" {
			return append(pos, rest...), nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// printFlags lists flags GNU-style (--name VALUE), which reads better than
// flag.PrintDefaults for long option names.
func printFlags(w io.Writer, fs *flag.FlagSet) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fs.VisitAll(func(f *flag.Flag) {
		arg, help := flag.UnquoteUsage(f)
		spec := "--" + f.Name
		if arg != "" {
			spec += " " + arg
		}
		switch f.DefValue {
		case "", "0", "false", "[]":
		default:
			help += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		fmt.Fprintf(tw, "  %s\t%s\n", spec, help)
	})
	fmt.Fprintf(tw, "  -h, --help\tshow this help\n")
	tw.Flush()
}

// measureFlags are the walker options shared by du, once and watch.
type measureFlags struct {
	apparent bool
	workers  int
}

func (m *measureFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&m.apparent, "apparent", false, "count apparent file sizes instead of allocated disk usage")
	fs.IntVar(&m.workers, "workers", 0, "concurrent directory walkers (0 picks a default from the CPU count)")
}

func (m *measureFlags) options() walk.Options {
	return walk.Options{Apparent: m.apparent, Workers: m.workers}
}

// historyFlags locate the history file.
type historyFlags struct {
	file string
}

func (h *historyFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&h.file, "history", "", "history `FILE` (default $SIZEWATCH_HISTORY, else $XDG_STATE_HOME/sizewatch/history.jsonl)")
}

func (h *historyFlags) open() (*history.Store, error) {
	if h.file != "" {
		return history.Open(h.file), nil
	}
	p, err := history.DefaultPath()
	if err != nil {
		return nil, err
	}
	return history.Open(p), nil
}

// absPaths makes paths absolute, since they key the history, and drops
// duplicates.
func absPaths(paths []string) ([]string, error) {
	seen := make(map[string]bool)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out, nil
}
