package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/trigger"
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
	interval := durationValue(5 * time.Minute)
	fs.Var(&interval, "interval", "time between samples, e.g. 30s, 5m, 1h")
	cooldown := durationValue(30 * time.Minute)
	fs.Var(&cooldown, "cooldown", "after an alert, hold back further alerts for that path for `DURATION` (0 disables)")
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

	store, err := hf.open()
	if err != nil {
		return err
	}
	samples, err := store.Load()
	if err != nil {
		return err
	}
	m := &monitor{
		a:        a,
		store:    store,
		measure:  mf.options(),
		triggers: tf.config(),
		hook:     tf.hook,
		hookOut:  a.stdout,
		cooldown: trigger.NewCooldown(time.Duration(cooldown)),
		prev:     history.Latest(samples),
	}
	if !*quiet {
		fmt.Fprintf(a.stderr, "sizewatch: watching %d %s every %s, history in %s\n",
			len(paths), plural(int64(len(paths)), "path", "paths"), units.FormatDuration(time.Duration(interval)), store.Path())
	}

	tick := time.NewTicker(time.Duration(interval))
	defer tick.Stop()
	for {
		outs, err := m.round(ctx, paths)
		if ctx.Err() != nil {
			return nil
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
