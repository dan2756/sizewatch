package main

import (
	"context"
	"fmt"

	"github.com/dan2756/sizewatch/internal/units"
	"github.com/dan2756/sizewatch/internal/walk"
)

func (a *app) du(ctx context.Context, args []string) error {
	fs := newFlagSet("du", "PATH... [flags]", "Print the disk usage of each path.")
	var mf measureFlags
	mf.register(fs)
	paths, err := a.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return usagef("du", "missing PATH")
	}

	failed := false
	for _, p := range paths {
		res, err := walk.Measure(ctx, p, mf.options())
		if err != nil {
			a.errorf("%v", err)
			failed = true
			continue
		}
		a.warnSkipped(p, res.Warnings)
		fmt.Fprintf(a.stdout, "%9s  %s\n", units.FormatSize(res.Bytes), p)
	}
	if failed {
		return errReported
	}
	return nil
}

// warnSkipped mentions entries the walker could not read.
func (a *app) warnSkipped(path string, n int64) {
	if n == 0 {
		return
	}
	noun := "entries"
	if n == 1 {
		noun = "entry"
	}
	a.errorf("warning: %s: skipped %d unreadable %s", path, n, noun)
}
