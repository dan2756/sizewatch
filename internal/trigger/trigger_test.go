package trigger

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
)

const (
	mb = 1_000_000
	gb = 1_000_000_000
)

var t0 = time.Date(2026, 8, 12, 19, 0, 0, 0, time.UTC)

// sample is a measurement of a folder with a file in it.
func sample(bytes int64, after time.Duration) history.Sample {
	return history.Sample{Time: t0.Add(after), Path: "/data", Bytes: bytes, Files: 1}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		prev *history.Sample
		cur  history.Sample
		want []Reason
	}{
		{"nothing enabled", Config{}, ptr(sample(1*gb, 0)), sample(9*gb, 5*time.Minute), nil},
		{"spike below", Config{Spike: 500 * mb}, ptr(sample(1*gb, 0)), sample(1*gb+499*mb, 5*time.Minute), nil},
		{"spike exact", Config{Spike: 500 * mb}, ptr(sample(1*gb, 0)), sample(1*gb+500*mb, 5*time.Minute), []Reason{Spike}},
		{"spike above", Config{Spike: 500 * mb}, ptr(sample(1*gb, 0)), sample(2*gb, 5*time.Minute), []Reason{Spike}},
		{"spike needs prev", Config{Spike: 1}, nil, sample(2*gb, 0), nil},
		{"shrink never spikes", Config{Spike: 1, SpikePct: 1}, ptr(sample(2*gb, 0)), sample(1*gb, 5*time.Minute), nil},
		{"no change", Config{Spike: 1, SpikePct: 1}, ptr(sample(2*gb, 0)), sample(2*gb, 5*time.Minute), nil},
		{"pct below", Config{SpikePct: 20}, ptr(sample(1*gb, 0)), sample(1*gb+199*mb, 5*time.Minute), nil},
		{"pct exact", Config{SpikePct: 20}, ptr(sample(1*gb, 0)), sample(1*gb+200*mb, 5*time.Minute), []Reason{Pct}},
		{"pct from empty", Config{SpikePct: 20}, ptr(sample(0, 0)), sample(4096, 5*time.Minute), nil},
		// An empty directory still takes a block on ext4 and tmpfs.
		{"pct from empty dir block", Config{SpikePct: 20}, ptr(history.Sample{Time: t0, Path: "/data", Bytes: 4096}), sample(100_000, 5*time.Minute), nil},
		{"spike from empty", Config{Spike: 1 * mb, SpikePct: 20}, ptr(sample(0, 0)), sample(2*mb, 5*time.Minute), []Reason{Spike}},
		{"pct fractional", Config{SpikePct: 0.5}, ptr(sample(1*gb, 0)), sample(1*gb+5*mb, time.Hour), []Reason{Pct}},
		{"rate below", Config{Rate: gb / 3600}, ptr(sample(1*gb, 0)), sample(1*gb+80*mb, 5*time.Minute), nil},
		{"rate reached", Config{Rate: gb / 3600}, ptr(sample(1*gb, 0)), sample(1*gb+84*mb, 5*time.Minute), []Reason{Rate}},
		{"rate over a long gap", Config{Rate: gb / 3600}, ptr(sample(1*gb, 0)), sample(3*gb, 3*time.Hour), nil},
		{"rate needs elapsed time", Config{Rate: 1}, ptr(sample(1*gb, 0)), sample(2*gb, 0), nil},
		{"rate ignores shrink", Config{Rate: 1}, ptr(sample(2*gb, 0)), sample(1*gb, time.Minute), nil},
		{"max below", Config{Max: 10 * gb}, ptr(sample(9*gb, 0)), sample(10*gb-1, 5*time.Minute), nil},
		{"max reached", Config{Max: 10 * gb}, ptr(sample(9*gb, 0)), sample(10*gb, 5*time.Minute), []Reason{Max}},
		{"max without prev", Config{Max: 10 * gb}, nil, sample(11*gb, 0), []Reason{Max}},
		{"max while shrinking", Config{Max: 10 * gb}, ptr(sample(12*gb, 0)), sample(11*gb, 5*time.Minute), []Reason{Max}},
		{
			"all at once",
			Config{Spike: 500 * mb, SpikePct: 10, Rate: gb / 3600, Max: 10 * gb},
			ptr(sample(9*gb, 0)), sample(11*gb, 5*time.Minute),
			[]Reason{Spike, Pct, Rate, Max},
		},
		{
			"only some",
			Config{Spike: 500 * mb, SpikePct: 50, Max: 100 * gb},
			ptr(sample(9*gb, 0)), sample(11*gb, 5*time.Minute),
			[]Reason{Spike},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.cfg, tt.prev, tt.cur)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestEventEnv(t *testing.T) {
	ev := Event{
		Prev:    ptr(sample(1*gb, 0)),
		Cur:     sample(1*gb+600*mb, 5*time.Minute),
		Reasons: []Reason{Spike, Pct},
	}
	want := []string{
		"SIZEWATCH_PATH=/data",
		"SIZEWATCH_BYTES=1600000000",
		"SIZEWATCH_BYTES_HUMAN=1.6 GB",
		"SIZEWATCH_PREV_BYTES=1000000000",
		"SIZEWATCH_DELTA_BYTES=600000000",
		"SIZEWATCH_DELTA_HUMAN=+600.0 MB",
		"SIZEWATCH_REASON=spike",
		"SIZEWATCH_REASONS=spike,pct",
	}
	if got := ev.Env(); !reflect.DeepEqual(got, want) {
		t.Errorf("Env() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	first := Event{Cur: sample(11*gb, 0), Reasons: []Reason{Max}}
	env := strings.Join(first.Env(), "\n")
	for _, kv := range []string{"SIZEWATCH_PREV_BYTES=\n", "SIZEWATCH_DELTA_BYTES=0\n", "SIZEWATCH_REASON=max\n"} {
		if !strings.Contains(env+"\n", kv) {
			t.Errorf("first-sample env lacks %q:\n%s", kv, env)
		}
	}
}

func TestRunHook(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env")
	ev := Event{Prev: ptr(sample(1*gb, 0)), Cur: sample(2*gb, time.Minute), Reasons: []Reason{Spike}}
	var stdout, stderr bytes.Buffer
	cmd := `echo "$SIZEWATCH_PATH grew by $SIZEWATCH_DELTA_HUMAN ($SIZEWATCH_REASON)"; echo "$SIZEWATCH_BYTES" > "$OUT"`
	t.Setenv("OUT", out)
	if err := RunHook(context.Background(), cmd, ev, &stdout, &stderr); err != nil {
		t.Fatalf("RunHook: %v (stderr %q)", err, stderr.String())
	}
	if got, want := stdout.String(), "/data grew by +1.0 GB (spike)\n"; got != want {
		t.Errorf("hook stdout = %q, want %q", got, want)
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "2000000000\n" {
		t.Errorf("hook wrote %q, %v", data, err)
	}
}

// TestRunHookCancelKillsChildren checks that ending the context (watch
// being stopped) kills what the hook started, not just sh.
func TestRunHookCancelKillsChildren(t *testing.T) {
	dir := t.TempDir()
	started, late := filepath.Join(dir, "started"), filepath.Join(dir, "late")
	t.Setenv("STARTED", started)
	t.Setenv("LATE", late)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	ev := Event{Cur: sample(1, 0), Reasons: []Reason{Max}}
	go func() {
		done <- RunHook(ctx, `(sleep 1; echo late > "$LATE") & touch "$STARTED"; wait`, ev, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hook did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("RunHook succeeded although it was cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunHook did not return after cancel")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(late); err == nil {
		t.Error("a process started by the hook outlived the cancel")
	}
}

func TestRunHookFailure(t *testing.T) {
	ev := Event{Cur: sample(1, 0), Reasons: []Reason{Max}}
	err := RunHook(context.Background(), "exit 3", ev, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Errorf("err = %v, want exit status 3", err)
	}
}

func TestCooldown(t *testing.T) {
	c := NewCooldown(30 * time.Minute)
	steps := []struct {
		path  string
		after time.Duration
		want  bool
	}{
		{"/a", 0, true},
		{"/a", 5 * time.Minute, false},
		{"/b", 5 * time.Minute, true}, // per path
		{"/a", 29 * time.Minute, false},
		{"/a", 30 * time.Minute, true}, // period over: alerts and restarts it
		{"/a", 45 * time.Minute, false},
		{"/b", 34 * time.Minute, false},
		{"/b", 35 * time.Minute, true},
	}
	for _, s := range steps {
		if got := c.Allow(s.path, t0.Add(s.after)); got != s.want {
			t.Errorf("Allow(%s, +%v) = %v, want %v", s.path, s.after, got, s.want)
		}
	}

	off := NewCooldown(0)
	for i := range 3 {
		if !off.Allow("/a", t0.Add(time.Duration(i)*time.Second)) {
			t.Errorf("zero cooldown held back alert %d", i)
		}
	}
}

func TestCooldownPersists(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state", "cooldown.json")

	c, err := LoadCooldown(file, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save(file, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Fatal("saved although nothing alerted")
	}
	if !c.Allow("/a", t0) {
		t.Fatal("first alert held back")
	}
	if !c.Unsaved() {
		t.Error("Unsaved() = false after an alert")
	}
	if err := c.Save(file, t0); err != nil {
		t.Fatal(err)
	}
	if c.Unsaved() {
		t.Error("Unsaved() = true after saving")
	}

	// A later run (a new process, as with cron) still remembers /a.
	next, err := LoadCooldown(file, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next.Allow("/a", t0.Add(10*time.Minute)) {
		t.Error("cooldown forgotten between runs")
	}
	if !next.Allow("/b", t0.Add(10*time.Minute)) {
		t.Error("unrelated path held back")
	}

	// Another writer saved meanwhile: both entries survive the merge.
	other, _ := LoadCooldown(file, 30*time.Minute)
	other.Allow("/c", t0.Add(11*time.Minute))
	if err := other.Save(file, t0.Add(11*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := next.Save(file, t0.Add(12*time.Minute)); err != nil {
		t.Fatal(err)
	}
	final, _ := LoadCooldown(file, 30*time.Minute)
	for _, p := range []string{"/a", "/b", "/c"} {
		if final.Allow(p, t0.Add(15*time.Minute)) {
			t.Errorf("%s lost in merge", p)
		}
	}
	if !final.Allow("/a", t0.Add(31*time.Minute)) {
		t.Error("/a still held back after its period")
	}

	// Very old entries are pruned on save.
	final.Allow("/new", t0.Add(40*24*time.Hour))
	if err := final.Save(file, t0.Add(40*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), `"/b"`) || !strings.Contains(string(data), `"/new"`) {
		t.Errorf("after pruning: %s", data)
	}
}

func TestCooldownSavePreservesMode(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cooldown.json")
	c, err := LoadCooldown(file, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Allow("/a", t0) {
		t.Fatal("first alert held back")
	}
	if err := c.Save(file, t0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.Allow("/b", t0.Add(time.Minute)) {
		t.Fatal("second path held back")
	}
	if err := c.Save(file, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("cooldown file mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestLoadCooldownCorrupt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cooldown.json")
	if err := os.WriteFile(file, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCooldown(file, time.Hour)
	if err != nil || !c.Allow("/a", t0) {
		t.Errorf("corrupt state: %v", err)
	}
}
