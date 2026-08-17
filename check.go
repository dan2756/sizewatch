package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/trigger"
	"github.com/dan2756/sizewatch/internal/units"
	"github.com/dan2756/sizewatch/internal/walk"
)

// monitor takes samples, evaluates the triggers against the previous sample
// of each path and runs the hook. once runs a single round; watch runs one
// per interval.
type monitor struct {
	a        *app
	store    *history.Store
	measure  walk.Options
	triggers trigger.Config
	hook     string    // shell command; empty means just report
	hookOut  io.Writer // where the hook's stdout goes
	cooldown *trigger.Cooldown
	prev     map[string]history.Sample // latest sample per path
}

// outcome is what one round found for one path.
type outcome struct {
	Sample   history.Sample
	Prev     *history.Sample // nil for the first sample of a path
	Warnings int64
	Reasons  []trigger.Reason // triggers that fired
	Cooldown bool             // triggered, but the path alerted too recently
	HookErr  error
	Err      error // the path could not be measured
}

// round measures each path once, runs the hook for those that trigger and
// appends the samples to the history. Hooks run before the history is
// written so that a full disk still gets its alert.
func (m *monitor) round(ctx context.Context, paths []string) ([]outcome, error) {
	outs := make([]outcome, 0, len(paths))
	var fresh []history.Sample
	for _, p := range paths {
		start := m.a.now().UTC().Truncate(time.Second)
		res, err := walk.Measure(ctx, p, m.measure)
		if ctx.Err() != nil {
			return outs, ctx.Err()
		}
		o := outcome{Sample: history.Sample{Time: start, Path: p}}
		if err != nil {
			o.Err = err
			outs = append(outs, o)
			continue
		}
		o.Sample.Bytes, o.Sample.Files, o.Warnings = res.Bytes, res.Files, res.Warnings
		if prev, ok := m.prev[p]; ok {
			o.Prev = &prev
		}
		m.prev[p] = o.Sample
		fresh = append(fresh, o.Sample)
		o.Reasons = trigger.Evaluate(m.triggers, o.Prev, o.Sample)
		if len(o.Reasons) > 0 {
			m.alert(ctx, &o)
		}
		outs = append(outs, o)
	}
	if err := m.store.Append(fresh...); err != nil {
		return outs, fmt.Errorf("saving history: %w", err)
	}
	return outs, nil
}

// alert runs the hook for a triggered outcome unless the path is cooling
// down from an earlier alert.
func (m *monitor) alert(ctx context.Context, o *outcome) {
	if !m.cooldown.Allow(o.Sample.Path, m.a.now()) {
		o.Cooldown = true
		return
	}
	if m.hook != "" {
		ev := trigger.Event{Cur: o.Sample, Prev: o.Prev, Reasons: o.Reasons}
		o.HookErr = trigger.RunHook(ctx, m.hook, ev, m.hookOut, m.a.stderr)
	}
}

// fired reports whether the outcome alerted (as opposed to being held back).
func (o outcome) fired() bool { return len(o.Reasons) > 0 && !o.Cooldown }

// problems prints errors and warnings to stderr and returns false if a
// path could not be measured or a hook failed.
func (m *monitor) problems(outs []outcome) bool {
	ok := true
	for _, o := range outs {
		if o.Err != nil {
			m.a.errorf("%v", o.Err)
			ok = false
			continue
		}
		m.a.warnSkipped(o.Sample.Path, o.Warnings)
		if o.HookErr != nil {
			m.a.errorf("%s: %v", o.Sample.Path, o.HookErr)
			ok = false
		}
	}
	return ok
}

// table prints one line per measured path. A non-empty stamp starts each
// line. With quiet, only paths that alerted are listed.
func (m *monitor) table(outs []outcome, stamp string, quiet bool) error {
	tbl := newTable(m.a.stdout)
	for _, o := range outs {
		if o.Err != nil || quiet && !o.fired() {
			continue
		}
		cells := []string{o.Sample.Path, units.FormatSize(o.Sample.Bytes), describeChange(o), describeAlert(o)}
		if stamp != "" {
			cells = append([]string{stamp}, cells...)
		}
		tbl.row(cells...)
	}
	return tbl.flush()
}

type outcomeJSON struct {
	Time       time.Time        `json:"time"`
	Path       string           `json:"path"`
	Bytes      int64            `json:"bytes"`
	Files      int64            `json:"files"`
	PrevTime   *time.Time       `json:"prev_time"`
	PrevBytes  *int64           `json:"prev_bytes"`
	DeltaBytes int64            `json:"delta_bytes"`
	Warnings   int64            `json:"warnings"`
	Reasons    []trigger.Reason `json:"reasons"`
	Alert      string           `json:"alert,omitempty"` // "fired" or "cooldown"
	HookError  string           `json:"hook_error,omitempty"`
	Error      string           `json:"error,omitempty"`
}

func outcomesJSON(outs []outcome) []outcomeJSON {
	list := make([]outcomeJSON, 0, len(outs))
	for _, o := range outs {
		j := outcomeJSON{
			Time:     o.Sample.Time,
			Path:     o.Sample.Path,
			Bytes:    o.Sample.Bytes,
			Files:    o.Sample.Files,
			Warnings: o.Warnings,
			Reasons:  append([]trigger.Reason{}, o.Reasons...),
		}
		if o.Prev != nil {
			j.PrevTime, j.PrevBytes = &o.Prev.Time, &o.Prev.Bytes
			j.DeltaBytes = o.Sample.Bytes - o.Prev.Bytes
		}
		switch {
		case o.Cooldown:
			j.Alert = "cooldown"
		case o.fired():
			j.Alert = "fired"
		}
		if o.HookErr != nil {
			j.HookError = o.HookErr.Error()
		}
		if o.Err != nil {
			j.Error = o.Err.Error()
		}
		list = append(list, j)
	}
	return list
}

func describeChange(o outcome) string {
	if o.Prev == nil {
		return "first sample"
	}
	return units.FormatDelta(o.Sample.Bytes-o.Prev.Bytes) + " in " + formatSpan(o.Sample.Time.Sub(o.Prev.Time))
}

func describeAlert(o outcome) string {
	if len(o.Reasons) == 0 {
		return ""
	}
	parts := make([]string, len(o.Reasons))
	for i, r := range o.Reasons {
		parts[i] = string(r)
	}
	list := strings.Join(parts, ",")
	switch {
	case o.Cooldown:
		return list + " (cooldown)"
	case o.HookErr != nil:
		return "ALERT " + list + " (hook failed)"
	default:
		return "ALERT " + list
	}
}

// formatSpan renders a duration coarsely: 45s, 12m, 5h, 3d.
func formatSpan(d time.Duration) string {
	d = max(d, 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
