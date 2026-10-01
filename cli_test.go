package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dan2756/sizewatch/internal/history"
)

// cli runs sizewatch in-process with a fixed clock.
type cli struct {
	t       *testing.T
	now     time.Time
	history string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	return &cli{
		t:       t,
		now:     time.Date(2026, 8, 17, 22, 0, 0, 0, time.UTC),
		history: filepath.Join(t.TempDir(), "state", "history.jsonl"),
	}
}

func (c *cli) run(args ...string) (stdout, stderr string, code int) {
	c.t.Helper()
	var out, errb bytes.Buffer
	a := &app{stdout: &out, stderr: &errb, now: func() time.Time { return c.now }}
	if len(args) > 0 && args[0] != "du" {
		args = append(args, "--history", c.history)
	}
	code = a.run(context.Background(), args)
	return out.String(), errb.String(), code
}

func (c *cli) runJSON(v any, args ...string) {
	c.t.Helper()
	out, errOut, code := c.run(append(args, "--json")...)
	if code != 0 {
		c.t.Fatalf("sizewatch %v: exit %d\nstdout: %s\nstderr: %s", args, code, out, errOut)
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		c.t.Fatalf("sizewatch %v: bad JSON %q: %v", args, out, err)
	}
}

func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOnceSpikeRunsHook(t *testing.T) {
	c := newCLI(t)
	dir := filepath.Join(t.TempDir(), "downloads")
	write(t, filepath.Join(dir, "a"), 1000)
	hookOut := filepath.Join(t.TempDir(), "hook.txt")
	hook := `printf '%s|%s|%s' "$SIZEWATCH_REASON" "$SIZEWATCH_PATH" "$SIZEWATCH_PREV_BYTES" > ` + hookOut

	var first []outcomeJSON
	c.runJSON(&first, "once", dir, "--apparent", "--spike", "500KB", "--hook", hook)
	if len(first) != 1 || first[0].PrevBytes != nil || len(first[0].Reasons) != 0 || first[0].Path != dir {
		t.Fatalf("first once = %+v", first)
	}
	if _, err := os.Stat(hookOut); err == nil {
		t.Fatal("hook ran on the first sample")
	}

	write(t, filepath.Join(dir, "b"), 600_000)
	c.now = c.now.Add(5 * time.Minute)
	var second []outcomeJSON
	c.runJSON(&second, "once", dir, "--apparent", "--spike", "500KB", "--hook", hook)
	o := second[0]
	if o.PrevBytes == nil || *o.PrevBytes != first[0].Bytes || o.DeltaBytes < 600_000 {
		t.Errorf("second once = %+v", o)
	}
	if len(o.Reasons) != 1 || o.Reasons[0] != "spike" || o.Alert != "fired" {
		t.Errorf("reasons = %v, alert = %q; want [spike] fired", o.Reasons, o.Alert)
	}
	got, err := os.ReadFile(hookOut)
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	if want := "spike|" + dir + "|" + jsonInt(first[0].Bytes); string(got) != want {
		t.Errorf("hook saw %q, want %q", got, want)
	}

	// Five minutes later it is still cooling down (default 30m), even
	// though this is a separate run.
	if err := os.Remove(hookOut); err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(5 * time.Minute)
	write(t, filepath.Join(dir, "c"), 700_000)
	out, _, code := c.run("once", dir, "--apparent", "--spike", "500KB", "--hook", hook)
	if code != 0 || !strings.Contains(out, "spike (cooldown)") || !strings.Contains(out, "in 5m") {
		t.Errorf("once during cooldown (exit %d):\n%s", code, out)
	}
	if _, err := os.Stat(hookOut); err == nil {
		t.Error("hook ran during cooldown")
	}

	// Once the cooldown is over, it alerts again.
	c.now = c.now.Add(30 * time.Minute)
	write(t, filepath.Join(dir, "d"), 700_000)
	out, _, code = c.run("once", dir, "--apparent", "--spike", "500KB", "--hook", hook)
	if code != 0 || !strings.Contains(out, "ALERT spike") {
		t.Errorf("once after cooldown (exit %d):\n%s", code, out)
	}
	if _, err := os.Stat(hookOut); err != nil {
		t.Error("hook did not run after the cooldown")
	}
}

func TestOnceCooldownZero(t *testing.T) {
	c := newCLI(t)
	dir := t.TempDir()
	for i := range 3 {
		write(t, filepath.Join(dir, "f"+string(rune('0'+i))), 2000)
		var outs []outcomeJSON
		c.runJSON(&outs, "once", dir, "--apparent", "--max", "1KB", "--cooldown", "0")
		if outs[0].Alert != "fired" {
			t.Errorf("run %d: alert = %q, want fired", i, outs[0].Alert)
		}
		c.now = c.now.Add(time.Minute)
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestReport(t *testing.T) {
	c := newCLI(t)
	dir := t.TempDir()
	start := c.now
	for i := range 4 {
		write(t, filepath.Join(dir, "f"+string(rune('0'+i))), 10_000)
		c.run("once", dir, "--apparent", "--quiet")
		c.now = c.now.Add(30 * time.Minute)
	}

	var rep []reportJSON
	c.runJSON(&rep, "report")
	if len(rep) != 1 || rep[0].Path != dir || len(rep[0].Samples) != 4 {
		t.Fatalf("report = %+v", rep)
	}
	r := rep[0]
	if !r.Time.Equal(start.Add(90 * time.Minute)) {
		t.Errorf("latest time = %v", r.Time)
	}
	if ch, ok := r.Change["1h"].(float64); !ok || ch < 20_000 {
		t.Errorf("1h change = %v, want about 20000", r.Change["1h"])
	}
	if r.Change["24h"] != nil || r.Change["7d"] != nil {
		t.Errorf("24h/7d change = %v/%v, want null", r.Change["24h"], r.Change["7d"])
	}

	out, _, code := c.run("report", dir)
	if code != 0 || !strings.Contains(out, "TREND") || !strings.Contains(out, "▁") || !strings.Contains(out, "█") {
		t.Errorf("report table (exit %d):\n%s", code, out)
	}

	_, errOut, code := c.run("report", filepath.Join(dir, "elsewhere"))
	if code != exitError || !strings.Contains(errOut, "no history") {
		t.Errorf("report of unknown path: exit %d, stderr %q", code, errOut)
	}
}

func TestDuJSON(t *testing.T) {
	c := newCLI(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a"), 1234)
	write(t, filepath.Join(dir, "sub", "b"), 766)
	var res []duJSON
	c.runJSON(&res, "du", dir, "--apparent")
	if len(res) != 1 || res[0].Files != 2 || res[0].Dirs != 2 || res[0].Bytes < 2000 {
		t.Errorf("du = %+v", res)
	}
}

func TestExitCodes(t *testing.T) {
	c := newCLI(t)
	t.Setenv("SIZEWATCH_HISTORY", c.history)
	missing := filepath.Join(t.TempDir(), "missing")
	tests := []struct {
		args []string
		code int
	}{
		{nil, exitUsage},
		{[]string{"--version"}, exitOK},
		{[]string{"help"}, exitOK},
		{[]string{"frobnicate"}, exitUsage},
		{[]string{"du"}, exitUsage},
		{[]string{"du", "-h"}, exitOK},
		{[]string{"du", missing}, exitError},
		{[]string{"once", t.TempDir(), "--spike", "lots"}, exitUsage},
		{[]string{"once", missing}, exitError},
		{[]string{"watch", t.TempDir(), "--interval", "0"}, exitUsage},
	}
	for _, tt := range tests {
		var out, errb bytes.Buffer
		a := &app{stdout: &out, stderr: &errb, now: func() time.Time { return c.now }}
		if got := a.run(context.Background(), tt.args); got != tt.code {
			t.Errorf("sizewatch %q: exit %d, want %d (stderr %q)", tt.args, got, tt.code, errb.String())
		}
	}
}

func TestOnceCompactsHistory(t *testing.T) {
	c := newCLI(t)
	dir := t.TempDir()
	old := history.Open(c.history)
	var samples []history.Sample
	for h := 40 * 24; h > 0; h-- {
		samples = append(samples, history.Sample{Time: c.now.Add(-time.Duration(h) * time.Hour), Path: dir, Bytes: int64(h)})
	}
	if err := old.Append(samples...); err != nil {
		t.Fatal(err)
	}

	if _, errOut, code := c.run("once", dir, "--quiet", "--keep", "30d"); code != 0 {
		t.Fatalf("once: exit %d: %s", code, errOut)
	}
	got, err := old.Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := 30*24 + 1; len(got) != want {
		t.Errorf("history has %d samples after once --keep 30d, want %d", len(got), want)
	}
	if oldest := c.now.Sub(got[0].Time); oldest > 30*24*time.Hour {
		t.Errorf("oldest sample is %v old", oldest)
	}
}
