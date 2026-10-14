package history

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 8, 11, 19, 0, 0, 0, time.UTC)

func at(min int, path string, bytes int64) Sample {
	return Sample{Time: t0.Add(time.Duration(min) * time.Minute), Path: path, Bytes: bytes, Files: bytes / 100}
}

func TestRoundTrip(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "state", "sizewatch", "history.jsonl"))
	first := []Sample{at(0, "/a", 100), at(0, "/b & c", 2000)}
	second := []Sample{at(5, "/a", 150)}
	if err := s.Append(first...); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(second...); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := append(first, second...)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v\nwant %+v", got, want)
	}

	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	wantLine := `{"time":"2026-08-11T19:00:00Z","path":"/a","bytes":100,"files":1}`
	if line != wantLine {
		t.Errorf("first line = %s\nwant %s", line, wantLine)
	}
}

func TestLoadMissingFile(t *testing.T) {
	got, err := Open(filepath.Join(t.TempDir(), "nope.jsonl")).Load()
	if err != nil || len(got) != 0 {
		t.Errorf("Load() = %v, %v; want empty, nil", got, err)
	}
}

func TestLoadSkipsBadLinesAndSorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.jsonl")
	data := `{"time":"2026-08-11T19:10:00Z","path":"/a","bytes":3,"files":0}

not json
{"time":"2026-08-11T19:00:00Z","path":"/a","bytes":1,"files":0}
{"path":"/no-time","bytes":1}
{"time":"2026-08-11T19:05:00Z","path":"/a","bytes":2,"fi`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Open(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	var bytes []int64
	for _, s := range got {
		bytes = append(bytes, s.Bytes)
	}
	if !reflect.DeepEqual(bytes, []int64{1, 3}) {
		t.Errorf("loaded bytes %v, want [1 3]", bytes)
	}
}

func TestLatestForPathPaths(t *testing.T) {
	samples := []Sample{at(0, "/b", 1), at(0, "/a", 10), at(5, "/b", 2), at(5, "/a", 20), at(10, "/b", 3)}
	latest := Latest(samples)
	if latest["/a"].Bytes != 20 || latest["/b"].Bytes != 3 || len(latest) != 2 {
		t.Errorf("Latest() = %+v", latest)
	}
	if got := ForPath(samples, "/b"); len(got) != 3 || got[2].Bytes != 3 {
		t.Errorf("ForPath(/b) = %+v", got)
	}
	if got := Paths(samples); !reflect.DeepEqual(got, []string{"/a", "/b"}) {
		t.Errorf("Paths() = %v", got)
	}
}

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Setenv("SIZEWATCH_HISTORY", "")
	t.Setenv("XDG_STATE_HOME", "")
	check := func(want string) {
		t.Helper()
		got, err := DefaultPath()
		if err != nil || got != want {
			t.Errorf("DefaultPath() = %q, %v; want %q", got, err, want)
		}
	}
	check(filepath.Join(home, ".local", "state", "sizewatch", "history.jsonl"))

	t.Setenv("XDG_STATE_HOME", "relative/ignored")
	check(filepath.Join(home, ".local", "state", "sizewatch", "history.jsonl"))

	t.Setenv("XDG_STATE_HOME", "/var/state")
	check(filepath.Join("/var/state", "sizewatch", "history.jsonl"))

	t.Setenv("SIZEWATCH_HISTORY", "/tmp/custom.jsonl")
	check("/tmp/custom.jsonl")
}

// TestAppendAfterPartialLine checks that a fragment left by a write cut
// short (a full disk) doesn't swallow the next sample.
func TestAppendAfterPartialLine(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "h.jsonl"))
	if err := s.Append(at(0, "/a", 1)); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(s.Path(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"time":"2026-08-11T19:05:00Z","pa`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if err := s.Append(at(10, "/a", 3)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Bytes != 3 {
		t.Errorf("Load() = %+v, want the samples before and after the fragment", got)
	}
}

func TestStoredPath(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "h.jsonl"))
	paths := []string{"/a/café", "/a/caf\xe9", "/a/\xe2\x82x", "/a/\xff\xfe"}
	for i, p := range paths {
		if err := s.Append(at(i, p, 1)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range paths {
		if got[i].Path != StoredPath(p) {
			t.Errorf("%q reads back as %q, StoredPath gives %q", p, got[i].Path, StoredPath(p))
		}
	}
	if StoredPath(paths[0]) != paths[0] {
		t.Errorf("StoredPath changed a valid path")
	}
}
