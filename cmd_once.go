package main

import (
	"context"
	"time"
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
	hf.registerKeep(fs)
	quiet := fs.Bool("quiet", false, "print only alerts and problems")
	jsonOut := fs.Bool("json", false, "print the samples and alerts as JSON")
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

	m, err := a.newMonitor(&mf, &tf, &hf)
	if err != nil {
		return err
	}
	if *jsonOut {
		m.hookOut = a.stderr // keep stdout valid JSON
	}
	outs, err := m.round(ctx, paths)
	if err == nil {
		m.compact(time.Duration(hf.keep))
	}
	ok := m.problems(outs)
	if *jsonOut {
		if jerr := a.writeJSON(outcomesJSON(outs)); jerr != nil {
			return jerr
		}
	} else if terr := m.table(outs, "", *quiet); terr != nil {
		return terr
	}
	if err != nil {
		return err
	}
	if !ok {
		return errReported
	}
	return nil
}
