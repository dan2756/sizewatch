// Package history stores size samples in a JSON-lines file, one sample per
// line, so that runs of sizewatch can compare against earlier measurements.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Sample is one measurement of one path.
type Sample struct {
	Time  time.Time `json:"time"`
	Path  string    `json:"path"`
	Bytes int64     `json:"bytes"`
	Files int64     `json:"files"`
}

// DefaultPath returns where the history lives: $SIZEWATCH_HISTORY if set,
// else $XDG_STATE_HOME/sizewatch/history.jsonl, else
// ~/.local/state/sizewatch/history.jsonl.
func DefaultPath() (string, error) {
	if p := os.Getenv("SIZEWATCH_HISTORY"); p != "" {
		return p, nil
	}
	// The XDG spec says relative paths are invalid and must be ignored.
	if d := os.Getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "sizewatch", "history.jsonl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating history file: %w (set SIZEWATCH_HISTORY)", err)
	}
	return filepath.Join(home, ".local", "state", "sizewatch", "history.jsonl"), nil
}

// Store is a history file.
type Store struct {
	path string
}

// Open returns the store at path. Nothing is read or created until used.
func Open(path string) *Store { return &Store{path: path} }

// Path is the file backing the store.
func (s *Store) Path() string { return s.path }

// Append adds samples to the end of the file, creating the file and its
// directory if needed. The samples go out in a single write, under the
// same lock that compaction takes.
func (s *Store) Append(samples ...Sample) error {
	if len(samples) == 0 {
		return nil
	}
	buf, err := encode(samples)
	if err != nil {
		return err
	}
	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Load reads every sample, oldest first. A missing file is an empty
// history. Lines that don't parse, say from an interrupted write, are skipped.
func (s *Store) Load() ([]Sample, error) {
	f, err := os.Open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Sample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var smp Sample
		if json.Unmarshal(line, &smp) != nil || smp.Path == "" || smp.Time.IsZero() {
			continue
		}
		out = append(out, smp)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path, err)
	}
	// Concurrent writers can interleave slightly out of order.
	sortByTime(out)
	return out, nil
}

func encode(samples []Sample) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, smp := range samples {
		if err := enc.Encode(smp); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func sortByTime(samples []Sample) {
	slices.SortStableFunc(samples, func(a, b Sample) int { return a.Time.Compare(b.Time) })
}

// Latest returns the newest sample of each path in samples, which must be
// sorted oldest first.
func Latest(samples []Sample) map[string]Sample {
	m := make(map[string]Sample)
	for _, s := range samples {
		m[s.Path] = s
	}
	return m
}

// ForPath returns the samples of one path, keeping their order.
func ForPath(samples []Sample, path string) []Sample {
	var out []Sample
	for _, s := range samples {
		if s.Path == path {
			out = append(out, s)
		}
	}
	return out
}

// Paths lists the distinct paths in samples, sorted.
func Paths(samples []Sample) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range samples {
		if !seen[s.Path] {
			seen[s.Path] = true
			out = append(out, s.Path)
		}
	}
	slices.Sort(out)
	return out
}
