package main

import (
	"testing"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
)

func TestSparkline(t *testing.T) {
	tests := []struct {
		in   []int64
		want string
	}{
		{nil, ""},
		{[]int64{5}, "▁"},
		{[]int64{3, 3, 3}, "▁▁▁"},
		{[]int64{0, 7}, "▁█"},
		{[]int64{0, 1, 2, 3, 4, 5, 6, 7}, "▁▂▃▄▅▆▇█"},
		{[]int64{10, 5, 0}, "█▅▁"},
		{[]int64{1_000_000_000, 1_000_000_100, 1_000_000_050}, "▁█▅"},
	}
	for _, tt := range tests {
		if got := sparkline(tt.in); got != tt.want {
			t.Errorf("sparkline(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func every(start time.Time, step time.Duration, bytes ...int64) []history.Sample {
	out := make([]history.Sample, len(bytes))
	for i, b := range bytes {
		out[i] = history.Sample{Time: start.Add(time.Duration(i) * step), Path: "/p", Bytes: b}
	}
	return out
}

func changes(s summary) []any {
	out := make([]any, len(s.Changes))
	for i, c := range s.Changes {
		if c != nil {
			out[i] = *c
		}
	}
	return out
}

func TestSummarize(t *testing.T) {
	t0 := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)

	// Every 30 minutes for 2 hours: 1h is known, 24h and 7d are not.
	s := summarize(every(t0, 30*time.Minute, 100, 200, 300, 400, 500), 3)
	if got := changes(s); got[0] != int64(200) || got[1] != nil || got[2] != nil {
		t.Errorf("30m samples: changes = %v, want [200 <nil> <nil>]", got)
	}
	if len(s.Recent) != 3 || s.Recent[0].Bytes != 300 || s.Latest.Bytes != 500 {
		t.Errorf("recent = %+v, latest = %+v", s.Recent, s.Latest)
	}

	// Daily samples for 8 days: the 1h figure must not use a day-old sample.
	s = summarize(every(t0, 24*time.Hour, 1, 2, 3, 4, 5, 6, 7, 8), 24)
	if got := changes(s); got[0] != nil || got[1] != int64(1) || got[2] != int64(7) {
		t.Errorf("daily samples: changes = %v, want [<nil> 1 7]", got)
	}
	if len(s.Recent) != 8 {
		t.Errorf("recent has %d samples, want all 8", len(s.Recent))
	}

	// Hourly cron that ran a few minutes late is still close enough.
	late := []history.Sample{
		{Time: t0, Path: "/p", Bytes: 10},
		{Time: t0.Add(67 * time.Minute), Path: "/p", Bytes: 15},
	}
	if got := changes(summarize(late, 24)); got[0] != int64(5) {
		t.Errorf("late cron: 1h change = %v, want 5", got[0])
	}

	// A single sample has no change at all.
	if got := changes(summarize(every(t0, time.Minute, 42), 24)); got[0] != nil || got[1] != nil || got[2] != nil {
		t.Errorf("single sample: changes = %v", got)
	}
}
