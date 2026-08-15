package main

import (
	"context"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/trigger"
)

func (a *app) once(ctx context.Context, args []string) error {
	fs := newFlagSet("once", "PATH... [flags]",
		"Take one sample of each path, append it to the history and evaluate the\n"+
			"triggers against the previous sample. Meant for cron or launchd.")
	var mf measureFlags
	mf.register(fs)
	var tf triggerFlags
	tf.register(fs)
	var hf historyFlags
	hf.register(fs)
	quiet := fs.Bool("quiet", false, "print only alerts and problems")
	paths, err := a.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return usagef("once", "missing PATH")
	}
	if paths, err = absPaths(paths); err != nil {
		return err
	}

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
		cooldown: trigger.NewCooldown(0),
		prev:     history.Latest(samples),
	}
	outs, err := m.round(ctx, paths)
	ok := m.report(outs, "", *quiet)
	if err != nil {
		return err
	}
	if !ok {
		return errReported
	}
	return nil
}
