// Package trigger decides when a folder's growth deserves an alert and runs
// the user's hook when it does.
package trigger

import (
	"github.com/dan2756/sizewatch/internal/history"
)

// Reason names the trigger that fired. It is passed to hooks as
// SIZEWATCH_REASON.
type Reason string

const (
	Spike Reason = "spike" // grew by at least Config.Spike bytes
	Pct   Reason = "pct"   // grew by at least Config.SpikePct percent
	Rate  Reason = "rate"  // grew at least Config.Rate bytes per second
	Max   Reason = "max"   // is at least Config.Max bytes
)

// Config holds the thresholds. A zero field disables that trigger; any
// combination may be enabled.
type Config struct {
	Spike    int64   // growth in bytes since the previous sample
	SpikePct float64 // growth in percent since the previous sample
	Rate     float64 // growth in bytes per second since the previous sample
	Max      int64   // absolute ceiling in bytes
}

// Enabled reports whether any trigger is set.
func (c Config) Enabled() bool {
	return c.Spike > 0 || c.SpikePct > 0 || c.Rate > 0 || c.Max > 0
}

// Evaluate returns the triggers that fire for cur, in the order spike, pct,
// rate, max. prev is the previous sample of the same path, or nil if there is
// none, in which case only max can fire. Shrinking never fires growth
// triggers.
func Evaluate(c Config, prev *history.Sample, cur history.Sample) []Reason {
	var out []Reason
	if prev != nil {
		if delta := cur.Bytes - prev.Bytes; delta > 0 {
			if c.Spike > 0 && delta >= c.Spike {
				out = append(out, Spike)
			}
			// Any growth from nothing is "infinite percent"; that's what
			// --spike and --max are for, so pct needs a non-empty base.
			if c.SpikePct > 0 && prev.Bytes > 0 && float64(delta)*100/float64(prev.Bytes) >= c.SpikePct {
				out = append(out, Pct)
			}
			// Rate divides by the actual time between the samples, so it
			// means the same thing whatever the sampling interval.
			if elapsed := cur.Time.Sub(prev.Time); c.Rate > 0 && elapsed > 0 &&
				float64(delta)/elapsed.Seconds() >= c.Rate {
				out = append(out, Rate)
			}
		}
	}
	if c.Max > 0 && cur.Bytes >= c.Max {
		out = append(out, Max)
	}
	return out
}
