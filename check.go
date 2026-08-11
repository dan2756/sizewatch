package main

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/units"
	"github.com/dan2756/sizewatch/internal/walk"
)

// monitor takes samples and compares them with the previous ones. once runs
// a single round; watch runs one per interval.
type monitor struct {
	a       *app
	store   *history.Store
	measure walk.Options
	prev    map[string]history.Sample // latest sample per path
}

// outcome is what one round found for one path.
type outcome struct {
	Sample   history.Sample
	Prev     *history.Sample // nil for the first sample of a path
	Warnings int64
	Err      error // the path could not be measured
}

// round measures each path once and appends the samples to the history.
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
		outs = append(outs, o)
	}
	if err := m.store.Append(fresh...); err != nil {
		return outs, fmt.Errorf("saving history: %w", err)
	}
	return outs, nil
}

// report prints one line per path to stdout and problems to stderr. A
// non-empty stamp is printed first on each line. It returns false if any
// path failed.
func (m *monitor) report(outs []outcome, stamp string, quiet bool) bool {
	ok := true
	tw := tabwriter.NewWriter(m.a.stdout, 0, 0, 2, ' ', 0)
	for _, o := range outs {
		if o.Err != nil {
			m.a.errorf("%v", o.Err)
			ok = false
			continue
		}
		m.a.warnSkipped(o.Sample.Path, o.Warnings)
		if quiet {
			continue
		}
		if stamp != "" {
			fmt.Fprintf(tw, "%s\t", stamp)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", o.Sample.Path, units.FormatSize(o.Sample.Bytes), describeChange(o))
	}
	tw.Flush()
	return ok
}

func describeChange(o outcome) string {
	if o.Prev == nil {
		return "first sample"
	}
	return units.FormatDelta(o.Sample.Bytes-o.Prev.Bytes) + " in " + formatSpan(o.Sample.Time.Sub(o.Prev.Time))
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
