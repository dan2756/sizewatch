package main

import (
	"context"
	"fmt"

	"github.com/dan2756/sizewatch/internal/units"
	"github.com/dan2756/sizewatch/internal/walk"
)

func (a *app) du(ctx context.Context, args []string) error {
	fs := newFlagSet("du", "PATH... [flags]", "Print the disk usage of each path.")
	apparent := fs.Bool("apparent", false, "count apparent file sizes instead of allocated disk usage")
	workers := fs.Int("workers", 0, "concurrent directory walkers (0 picks a default from the CPU count)")
	paths, err := a.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return usagef("du", "missing PATH")
	}

	failed := false
	for _, p := range paths {
		res, err := walk.Measure(ctx, p, walk.Options{Apparent: *apparent, Workers: *workers})
		if err != nil {
			a.errorf("%v", err)
			failed = true
			continue
		}
		a.warnSkipped(res)
		fmt.Fprintf(a.stdout, "%9s  %s\n", units.FormatSize(res.Bytes), p)
	}
	if failed {
		return errReported
	}
	return nil
}

// warnSkipped mentions entries the walker could not read.
func (a *app) warnSkipped(res walk.Result) {
	if res.Warnings == 0 {
		return
	}
	noun := "entries"
	if res.Warnings == 1 {
		noun = "entry"
	}
	a.errorf("warning: %s: skipped %d unreadable %s", res.Path, res.Warnings, noun)
}
