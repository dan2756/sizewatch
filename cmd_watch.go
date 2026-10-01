package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dan2756/sizewatch/internal/units"
)

func (a *app) watch(ctx context.Context, args []string) error {
	fs := newFlagSet("watch", "PATH... [flags]",
		"Measure each path every interval, record the samples in the history and\n"+
			"run the hook when a trigger fires. Stops on Ctrl-C or SIGTERM.")
	var mf measureFlags
	mf.register(fs)
	var tf triggerFlags
	tf.register(fs)
	var hf historyFlags
	hf.register(fs)
	hf.registerKeep(fs)
	interval := durationValue(5 * time.Minute)
	fs.Var(&interval, "interval", "`DURATION` between samples, e.g. 30s, 5m, 1h")
	quiet := fs.Bool("quiet", false, "print only alerts and problems")
	paths, err := a.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return usagef("watch", "missing PATH")
	}
	if interval <= 0 {
		return usagef("watch", "--interval must be positive")
	}
	if paths, err = absPaths(paths); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	m, err := a.newMonitor(&mf, &tf, &hf)
	if err != nil {
		return err
	}
	if !*quiet {
		fmt.Fprintf(a.stderr, "sizewatch: watching %d %s every %s, history in %s\n",
			len(paths), plural(int64(len(paths)), "path", "paths"), units.FormatDuration(time.Duration(interval)), m.store.Path())
	}

	tick := time.NewTicker(time.Duration(interval))
	defer tick.Stop()
	var compacted time.Time
	for {
		outs, err := m.round(ctx, paths)
		if ctx.Err() != nil {
			return nil
		}
		if now := a.now(); now.Sub(compacted) >= time.Hour {
			m.compact(time.Duration(hf.keep))
			compacted = now
		}
		m.problems(outs)
		if terr := m.table(outs, a.now().Format("2006-01-02 15:04:05"), *quiet); terr != nil {
			return terr
		}
		if err != nil {
			// Most likely a full or read-only disk; keep watching.
			a.errorf("%v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
