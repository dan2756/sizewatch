package trigger

import "time"

// Cooldown remembers when each path last alerted, so that a folder that
// keeps growing alerts once per period rather than on every sample.
type Cooldown struct {
	Period time.Duration
	last   map[string]time.Time
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
	c.last[path] = now
	return true
}
