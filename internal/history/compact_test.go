package history

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// series returns a sample of each path every step from start up to now.
func series(start, now time.Time, step time.Duration, paths ...string) []Sample {
	var out []Sample
	for t := start; !t.After(now); t = t.Add(step) {
		for i, p := range paths {
			out = append(out, Sample{Time: t, Path: p, Bytes: t.Unix() + int64(i)})
		}
	}
	return out
}

func TestCompact(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	day := 24 * time.Hour

	tests := []struct {
		name    string
		samples []Sample
		keep    time.Duration
		want    int
	}{
		{"empty", nil, 30 * day, 0},
		{"recent samples untouched", series(now.Add(-23*time.Hour), now, 5*time.Minute, "/a"), 30 * day, 23*12 + 1},
		// 3 days every 5 minutes: the last 24h at full resolution (288 + the
		// sample exactly 24h old), the 48 hours before that hourly.
		{"older than a day thinned", series(now.Add(-3*day), now, 5*time.Minute, "/a"), 30 * day, 289 + 48},
		{"paths thinned separately", series(now.Add(-3*day), now, 5*time.Minute, "/a", "/b"), 30 * day, 2 * (289 + 48)},
		{"keep drops old", series(now.Add(-10*day), now, time.Hour, "/a"), 7 * day, 7*24 + 1},
		{"keep 0 keeps all ages", series(now.Add(-100*day), now, time.Hour, "/a"), 0, 100*24 + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compact(tt.samples, now, tt.keep)
			if len(got) != tt.want {
				t.Fatalf("kept %d of %d samples, want %d", len(got), len(tt.samples), tt.want)
			}
			for i := 1; i < len(got); i++ {
				if got[i].Time.Before(got[i-1].Time) {
					t.Fatalf("result not sorted at %d", i)
				}
			}
			if again := Compact(got, now, tt.keep); len(again) != len(got) {
				t.Errorf("compacting twice kept %d, then %d", len(got), len(again))
			}
		})
	}
}

func TestCompactKeepsLatestInHour(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	in := []Sample{
		{Time: old.Add(50 * time.Minute), Path: "/a", Bytes: 3},
		{Time: old.Add(10 * time.Minute), Path: "/a", Bytes: 1},
		{Time: old.Add(20 * time.Minute), Path: "/b", Bytes: 9},
		{Time: old.Add(30 * time.Minute), Path: "/a", Bytes: 2},
		{Time: old.Add(70 * time.Minute), Path: "/a", Bytes: 4},
	}
	got := Compact(in, now, 0)
	want := []Sample{in[2], in[0], in[4]}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Compact() = %+v\nwant %+v", got, want)
	}
	if in[0].Bytes != 3 || in[1].Bytes != 1 {
		t.Error("Compact modified its input")
	}
}

func TestStoreCompact(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	s := Open(filepath.Join(t.TempDir(), "history.jsonl"))
	all := series(now.Add(-40*24*time.Hour), now, time.Hour, "/a")
	if err := s.Append(all...); err != nil {
		t.Fatal(err)
	}

	// Too little to drop: the file is left alone.
	before, _ := os.ReadFile(s.Path())
	if n, err := s.Compact(now, 39*24*time.Hour+30*time.Minute, 100); err != nil || n != 0 {
		t.Fatalf("Compact() = %d, %v; want 0, nil", n, err)
	}
	if after, _ := os.ReadFile(s.Path()); string(after) != string(before) {
		t.Fatal("file rewritten although nothing worth dropping")
	}

	n, err := s.Compact(now, 30*24*time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 10*24 {
		t.Errorf("dropped %d samples, want %d", n, 10*24)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := Compact(all, now, 30*24*time.Hour); !reflect.DeepEqual(got, want) {
		t.Errorf("after compaction the file holds %d samples, want %d", len(got), len(want))
	}
	if fi, err := os.Stat(s.Path()); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("compacted file mode = %v, %v", fi.Mode(), err)
	}
	matches, _ := filepath.Glob(s.Path() + ".tmp*")
	if len(matches) != 0 {
		t.Errorf("left temp files behind: %v", matches)
	}

	// Appending still works on the rewritten file.
	if err := s.Append(Sample{Time: now.Add(time.Minute), Path: "/a", Bytes: 1}); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Load(); len(again) != len(got)+1 {
		t.Errorf("after append: %d samples, want %d", len(again), len(got)+1)
	}
}

// TestCompactDuringAppends checks that the lock keeps compaction from
// losing samples that other writers append meanwhile.
func TestCompactDuringAppends(t *testing.T) {
	now := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	s := Open(filepath.Join(t.TempDir(), "history.jsonl"))
	if err := s.Append(series(now.Add(-60*24*time.Hour), now.Add(-31*24*time.Hour), time.Hour, "/old")...); err != nil {
		t.Fatal(err)
	}

	const writers, each = 4, 25
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range each {
				smp := Sample{Time: now, Path: fmt.Sprintf("/w%d", w), Bytes: int64(i)}
				if err := Open(s.Path()).Append(smp); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for range 10 {
		if _, err := Open(s.Path()).Compact(now, 30*24*time.Hour, 1); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	if _, err := s.Compact(now, 30*24*time.Hour, 1); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != writers*each {
		t.Errorf("history holds %d samples, want the %d appended ones", len(got), writers*each)
	}
}
