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
	jsonOut := fs.Bool("json", false, "print JSON instead of text")
	paths, err := a.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return usagef("du", "missing PATH")
	}

	failed := false
	var results []duJSON
	for _, p := range paths {
		res, err := walk.Measure(ctx, p, mf.options())
		if err != nil {
			a.errorf("%v", err)
			failed = true
			if *jsonOut {
				results = append(results, duJSON{Path: p, Error: err.Error()})
			}
			continue
		}
		a.warnSkipped(p, res.Warnings)
		if *jsonOut {
			results = append(results, duJSON{Path: p, Bytes: res.Bytes, Files: res.Files, Dirs: res.Dirs, Warnings: res.Warnings})
			continue
		}
		fmt.Fprintf(a.stdout, "%9s  %s\n", units.FormatSize(res.Bytes), p)
	}
	if *jsonOut {
		if err := a.writeJSON(results); err != nil {
			return err
		}
	}
	if failed {
		return errReported
	}
	return nil
}

type duJSON struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	Files    int64  `json:"files"`
	Dirs     int64  `json:"dirs"`
	Warnings int64  `json:"warnings"`
	Error    string `json:"error,omitempty"`
}

// warnSkipped mentions entries the walker could not read.
func (a *app) warnSkipped(path string, n int64) {
	if n == 0 {
		return
	}
	a.errorf("warning: %s: skipped %d unreadable %s", path, n, plural(n, "entry", "entries"))
}
