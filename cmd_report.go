package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
	"github.com/dan2756/sizewatch/internal/units"
)

// windows are the spans report shows the change over.
var windows = []struct {
	label string
	span  time.Duration
}{
	{"1h", time.Hour},
	{"24h", 24 * time.Hour},
	{"7d", 7 * 24 * time.Hour},
}

func (a *app) report(ctx context.Context, args []string) error {
	fs := newFlagSet("report", "[PATH...] [flags]",
		"Show each path's latest size, its change over the last 1h, 24h and 7d,\n"+
			"and a sparkline of the recent samples. Without PATH, every path in the\n"+
			"history is shown.")
	var hf historyFlags
	hf.register(fs)
	points := fs.Int("points", 24, "number of recent samples in the sparkline")
	jsonOut := fs.Bool("json", false, "print JSON instead of a table")
	paths, err := a.parseArgs(fs, args)
	if err != nil {
		return err
	}
	if *points < 1 {
		return usagef("report", "--points must be at least 1")
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
	failed := false
	if len(paths) == 0 {
		paths = history.Paths(samples)
		if len(paths) == 0 && !*jsonOut {
			a.errorf("no samples in %s yet; run 'sizewatch once PATH' or 'sizewatch watch PATH'", store.Path())
			return nil
		}
	}
	sums := make([]summary, 0, len(paths))
	for _, p := range paths {
		ps := history.ForPath(samples, p)
		if len(ps) == 0 {
			a.errorf("no history for %s", p)
			failed = true
			continue
		}
		sums = append(sums, summarize(ps, *points))
	}

	if *jsonOut {
		err = a.printReportJSON(sums)
	} else {
		err = a.printReport(sums)
	}
	if err != nil {
		return err
	}
	if failed {
		return errReported
	}
	return nil
}

// summary is what report shows for one path.
type summary struct {
	Latest  history.Sample
	Changes []*int64 // per window; nil where the history doesn't reach back
	Recent  []history.Sample
}

// summarize describes the samples of one path, which must be sorted oldest
// first and not be empty. Changes are measured from the latest sample, not
// from the current time, so a report on an idle history still makes sense.
func summarize(samples []history.Sample, points int) summary {
	latest := samples[len(samples)-1]
	s := summary{Latest: latest, Changes: make([]*int64, len(windows))}
	for i, w := range windows {
		// Accept a reference sample up to half a window older than the
		// target, so hourly cron samples still give a 24h figure but a
		// day-old sample isn't passed off as the 1h change.
		if ref, ok := sampleBefore(samples, latest.Time.Add(-w.span), w.span/2); ok {
			d := latest.Bytes - ref.Bytes
			s.Changes[i] = &d
		}
	}
	s.Recent = samples[max(0, len(samples)-points):]
	return s
}

// sampleBefore returns the newest sample taken at or before target, if it
// is at most slack older than target.
func sampleBefore(samples []history.Sample, target time.Time, slack time.Duration) (history.Sample, bool) {
	i := sort.Search(len(samples), func(i int) bool { return samples[i].Time.After(target) })
	if i == 0 || target.Sub(samples[i-1].Time) > slack {
		return history.Sample{}, false
	}
	return samples[i-1], true
}

var sparkBars = []rune("▁▂▃▄▅▆▇█")

// sparkline draws values scaled between their minimum and maximum. A flat
// series is drawn as a flat baseline.
func sparkline(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = min(lo, v), max(hi, v)
	}
	var b strings.Builder
	for _, v := range values {
		i := 0
		if hi > lo {
			i = int(float64(v-lo)/float64(hi-lo)*float64(len(sparkBars)-1) + 0.5)
		}
		b.WriteRune(sparkBars[i])
	}
	return b.String()
}

func (a *app) printReport(sums []summary) error {
	tbl := newTable(a.stdout)
	header := []string{"PATH", "SIZE", "FILES"}
	for _, w := range windows {
		header = append(header, strings.ToUpper(w.label))
	}
	tbl.row(append(header, "UPDATED", "TREND")...)
	now := a.now()
	for _, s := range sums {
		cells := []string{s.Latest.Path, units.FormatSize(s.Latest.Bytes), strconv.FormatInt(s.Latest.Files, 10)}
		for _, c := range s.Changes {
			if c == nil {
				cells = append(cells, "-")
			} else {
				cells = append(cells, units.FormatDelta(*c))
			}
		}
		values := make([]int64, len(s.Recent))
		for i, r := range s.Recent {
			values[i] = r.Bytes
		}
		cells = append(cells, formatSpan(now.Sub(s.Latest.Time))+" ago", sparkline(values))
		tbl.row(cells...)
	}
	return tbl.flush()
}

type reportJSON struct {
	Path    string            `json:"path"`
	Time    time.Time         `json:"time"`
	Bytes   int64             `json:"bytes"`
	Files   int64             `json:"files"`
	Change  map[string]any    `json:"change"`
	Samples []reportPointJSON `json:"samples"`
}

type reportPointJSON struct {
	Time  time.Time `json:"time"`
	Bytes int64     `json:"bytes"`
}

func (a *app) printReportJSON(sums []summary) error {
	out := make([]reportJSON, 0, len(sums))
	for _, s := range sums {
		r := reportJSON{
			Path:   s.Latest.Path,
			Time:   s.Latest.Time,
			Bytes:  s.Latest.Bytes,
			Files:  s.Latest.Files,
			Change: make(map[string]any, len(windows)),
		}
		for i, w := range windows {
			if c := s.Changes[i]; c != nil {
				r.Change[w.label] = *c
			} else {
				r.Change[w.label] = nil
			}
		}
		for _, p := range s.Recent {
			r.Samples = append(r.Samples, reportPointJSON{Time: p.Time, Bytes: p.Bytes})
		}
		out = append(out, r)
	}
	return a.writeJSON(out)
}

// writeJSON prints v as indented JSON on stdout.
func (a *app) writeJSON(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("writing JSON: %w", err)
	}
	return nil
}
