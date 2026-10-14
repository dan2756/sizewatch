package trigger

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cooldown remembers when each path last alerted, so that a folder that
// keeps growing alerts once per period rather than on every sample.
type Cooldown struct {
	Period time.Duration
	last   map[string]time.Time
	dirty  bool // an alert was recorded since loading or saving
}

// NewCooldown returns a cooldown of period; zero or less never holds back.
func NewCooldown(period time.Duration) *Cooldown {
	return &Cooldown{Period: period, last: make(map[string]time.Time)}
}

// Allow reports whether path may alert at now and, if so, starts a new
// cooldown period for it.
func (c *Cooldown) Allow(path string, now time.Time) bool {
	if last, ok := c.last[path]; ok && c.Period > 0 && now.Sub(last) < c.Period {
		return false
	}
	c.last[path] = now.UTC().Truncate(time.Second) // tidier state file
	c.dirty = true
	return true
}

// LoadCooldown restores the alert times saved at file, so that separate
// runs of once (or a restarted watch) honor the period. A missing file
// starts empty, and so does a corrupt one: the worst outcome is an alert
// arriving early.
func LoadCooldown(file string, period time.Duration) (*Cooldown, error) {
	c := NewCooldown(period)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(data, &c.last) != nil || c.last == nil {
		c.last = make(map[string]time.Time)
	}
	return c, nil
}

// Unsaved reports whether an alert was recorded since loading or saving.
func (c *Cooldown) Unsaved() bool { return c.dirty }

// Save writes the alert times to file if any alert was recorded. It merges
// with what is already there, keeping the latest time per path, because a
// watch and a cron job may share the file; the caller must keep other
// writers out until it returns. Entries older than both the period and 30
// days are dropped.
func (c *Cooldown) Save(file string, now time.Time) error {
	if !c.dirty {
		return nil
	}
	merged := make(map[string]time.Time)
	if data, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(data, &merged)
	}
	for p, t := range c.last {
		if t.After(merged[p]) {
			merged[p] = t
		}
	}
	horizon := max(c.Period, 30*24*time.Hour)
	for p, t := range merged {
		if now.Sub(t) > horizon {
			delete(merged, p)
		}
	}
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(file, append(data, '\n')); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

func writeFileAtomic(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
