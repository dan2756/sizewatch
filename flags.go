package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/trigger"
	"github.com/dan2756/sizewatch/internal/units"
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
	exclude  patternList
	oneFS    bool
	workers  int
}

func (m *measureFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&m.apparent, "apparent", false, "count apparent file sizes instead of allocated disk usage")
	fs.Var(&m.exclude, "exclude", "skip entries matching `GLOB` (by name, or by relative path if it has a slash; repeatable)")
	fs.BoolVar(&m.oneFS, "one-file-system", false, "don't descend into directories on other file systems")
	fs.IntVar(&m.workers, "workers", 0, "concurrent directory walkers (0 picks a default from the CPU count)")
}

func (m *measureFlags) options() walk.Options {
	return walk.Options{
		Apparent:      m.apparent,
		Exclude:       m.exclude,
		OneFileSystem: m.oneFS,
		Workers:       m.workers,
	}
}

// triggerFlags configure alerts for once and watch.
type triggerFlags struct {
	spike sizeValue
	pct   percentValue
	rate  rateValue
	max   sizeValue
	hook  string
}

func (t *triggerFlags) register(fs *flag.FlagSet) {
	fs.Var(&t.spike, "spike", "alert when a path grows by `SIZE` since the previous sample (e.g. 500MB)")
	fs.Var(&t.pct, "spike-pct", "alert when a path grows by `PERCENT` since the previous sample")
	fs.Var(&t.rate, "rate", "alert when a path grows faster than `RATE` between samples (e.g. 1GB/h)")
	fs.Var(&t.max, "max", "alert when a path is at least `SIZE` (e.g. 10GB)")
	fs.StringVar(&t.hook, "hook", "", "run `COMMAND` with sh -c when an alert fires (see SIZEWATCH_* variables)")
}

func (t *triggerFlags) config() trigger.Config {
	return trigger.Config{
		Spike:    int64(t.spike),
		SpikePct: float64(t.pct),
		Rate:     float64(t.rate),
		Max:      int64(t.max),
	}
}

// historyFlags locate the history file and, for commands that write to it,
// bound its size.
type historyFlags struct {
	file string
	keep durationValue
}

func (h *historyFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&h.file, "history", "", "history `FILE` (default $SIZEWATCH_HISTORY, else $XDG_STATE_HOME/sizewatch/history.jsonl)")
}

func (h *historyFlags) registerKeep(fs *flag.FlagSet) {
	h.keep = durationValue(30 * 24 * time.Hour)
	fs.Var(&h.keep, "keep", "drop samples older than `DURATION` from the history; 0 keeps everything")
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

// sizeValue is a flag holding a byte count written like 500MB or 2GiB.
type sizeValue int64

func (v *sizeValue) Set(s string) error {
	n, err := units.ParseSize(s)
	if err != nil {
		return err
	}
	*v = sizeValue(n)
	return nil
}

func (v *sizeValue) String() string {
	if v == nil || *v == 0 {
		return ""
	}
	return units.FormatSize(int64(*v))
}

// percentValue is a flag holding a positive percentage, with or without "%".
type percentValue float64

func (v *percentValue) Set(s string) error {
	f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	if err != nil || f <= 0 {
		return fmt.Errorf("invalid percentage %q", s)
	}
	*v = percentValue(f)
	return nil
}

func (v *percentValue) String() string {
	if v == nil || *v == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(*v), 'g', -1, 64)
}

// rateValue is a flag holding a growth rate like 1GB/h, in bytes per second.
type rateValue float64

func (v *rateValue) Set(s string) error {
	r, err := units.ParseRate(s)
	if err != nil {
		return err
	}
	if r <= 0 {
		return fmt.Errorf("invalid rate %q: must be positive", s)
	}
	*v = rateValue(r)
	return nil
}

func (v *rateValue) String() string {
	if v == nil || *v == 0 {
		return ""
	}
	return units.FormatRate(float64(*v))
}

// durationValue is a flag holding a duration; on top of Go's syntax it
// accepts days and weeks (30d, 1w).
type durationValue time.Duration

func (v *durationValue) Set(s string) error {
	d, err := units.ParseDuration(s)
	if err != nil {
		return err
	}
	*v = durationValue(d)
	return nil
}

func (v *durationValue) String() string {
	if v == nil {
		return ""
	}
	return units.FormatDuration(time.Duration(*v))
}

// patternList is a repeatable flag of exclude globs.
type patternList []string

func (p *patternList) Set(s string) error {
	if err := walk.ValidatePattern(s); err != nil {
		return err
	}
	*p = append(*p, s)
	return nil
}

func (p *patternList) String() string {
	if p == nil {
		return ""
	}
	return strings.Join(*p, ", ")
}
