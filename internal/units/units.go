// Package units parses and formats byte sizes, growth rates and durations.
//
// Size parsing accepts both bases: KB, MB, GB, TB and PB are decimal (powers of
// 1000) and KiB, MiB, GiB, TiB and PiB are binary (powers of 1024).
// Formatting always uses decimal units, so 1 GB = 1,000,000,000 bytes.
package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var sizeUnits = map[string]float64{
	"": 1, "b": 1,
	"k": 1e3, "kb": 1e3,
	"m": 1e6, "mb": 1e6,
	"g": 1e9, "gb": 1e9,
	"t": 1e12, "tb": 1e12,
	"p": 1e15, "pb": 1e15,
	"kib": 1 << 10,
	"mib": 1 << 20,
	"gib": 1 << 30,
	"tib": 1 << 40,
	"pib": 1 << 50,
}

// ParseSize parses a size such as "500MB", "1.5 GiB", "10gb" or "4096".
// Units are case-insensitive, a bare number is bytes, and the one-letter
// forms K, M, G, T and P are decimal like KB, MB and so on.
func ParseSize(s string) (int64, error) {
	t := strings.TrimSpace(s)
	i := 0
	for i < len(t) && (t[i] >= '0' && t[i] <= '9' || t[i] == '.') {
		i++
	}
	num, unit := t[:i], strings.TrimSpace(t[i:])
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	mult, ok := sizeUnits[strings.ToLower(unit)]
	if !ok {
		return 0, fmt.Errorf("invalid size %q: unknown unit %q (use B, KB, MB, GB, TB or KiB, MiB, GiB, TiB)", s, unit)
	}
	b := math.Round(v * mult)
	if b >= math.MaxInt64 {
		return 0, fmt.Errorf("invalid size %q: too large", s)
	}
	return int64(b), nil
}

var decimalUnits = []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}

// FormatSize renders n bytes for humans with decimal units, e.g. "512 B",
// "1.5 GB" or "-300.0 MB".
func FormatSize(n int64) string {
	sign := ""
	v := float64(n)
	if v < 0 {
		sign, v = "-", -v
	}
	if v < 1000 {
		return sign + strconv.FormatInt(int64(v), 10) + " B"
	}
	u := 0
	// 999.95 would print as "1000.0", so move up a unit before that.
	for v >= 999.95 && u < len(decimalUnits)-1 {
		v /= 1000
		u++
	}
	return sign + strconv.FormatFloat(v, 'f', 1, 64) + " " + decimalUnits[u]
}

// FormatDelta is FormatSize with an explicit sign, e.g. "+1.2 GB".
func FormatDelta(n int64) string {
	if n < 0 {
		return FormatSize(n)
	}
	return "+" + FormatSize(n)
}

const (
	day  = 24 * time.Hour
	week = 7 * day
)

// ParseDuration is time.ParseDuration plus the units d (24h) and w (7d),
// which may lead the value: "90s", "5m", "1h30m", "30d", "1w", "1d12h".
// Negative durations are rejected.
func ParseDuration(s string) (time.Duration, error) {
	t := strings.TrimSpace(s)
	bad := fmt.Errorf("invalid duration %q (examples: 90s, 5m, 12h, 30d, 1w)", s)
	if t == "" || strings.ContainsAny(t, "+-") {
		return 0, bad
	}
	var total time.Duration
	rest := t
	for _, u := range []struct {
		suffix byte
		unit   time.Duration
	}{{'w', week}, {'d', day}} {
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 || i == len(rest) || rest[i] != u.suffix {
			continue
		}
		n, err := strconv.ParseInt(rest[:i], 10, 64)
		if err != nil || n > int64(math.MaxInt64/u.unit) {
			return 0, bad
		}
		total, err = addDuration(total, time.Duration(n)*u.unit)
		if err != nil {
			return 0, bad
		}
		rest = rest[i+1:]
	}
	if rest != "" {
		d, err := time.ParseDuration(rest)
		if err != nil {
			return 0, bad
		}
		total, err = addDuration(total, d)
		if err != nil {
			return 0, bad
		}
	}
	return total, nil
}

// addDuration adds extra to total. Each piece can fit in an int64 while the
// sum does not (15250w48h); wrapping would turn a huge --cooldown or --keep
// into a negative duration, which disables them.
func addDuration(total, extra time.Duration) (time.Duration, error) {
	if extra < 0 || total > math.MaxInt64-extra {
		return 0, fmt.Errorf("duration overflow")
	}
	return total + extra, nil
}

// FormatDuration renders d compactly: "0", "90s", "5m", "1h30m", "30d".
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0"
	}
	if d%day == 0 {
		return strconv.FormatInt(int64(d/day), 10) + "d"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// ParseRate parses a growth rate written as SIZE/PERIOD, such as "1GB/h",
// "500MB/day", "10MiB/min" or "2GB/30m", and returns bytes per second.
// PERIOD is a unit (s, min, h, d, w and their long forms) or a duration.
func ParseRate(s string) (float64, error) {
	size, per, ok := strings.Cut(s, "/")
	if !ok {
		return 0, fmt.Errorf("invalid rate %q: want SIZE/PERIOD, e.g. 1GB/h", s)
	}
	n, err := ParseSize(size)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q: %w", s, err)
	}
	d, err := parsePeriod(per)
	if err != nil {
		return 0, fmt.Errorf("invalid rate %q: %w", s, err)
	}
	return float64(n) / d.Seconds(), nil
}

func parsePeriod(s string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "s", "sec", "second":
		return time.Second, nil
	case "m", "min", "minute":
		return time.Minute, nil
	case "h", "hr", "hour":
		return time.Hour, nil
	case "d", "day":
		return day, nil
	case "w", "wk", "week":
		return week, nil
	}
	d, err := ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("period must be positive")
	}
	return d, nil
}

// FormatRate renders bytes per second as growth per hour, e.g. "1.2 GB/h".
func FormatRate(bytesPerSec float64) string {
	return FormatSize(int64(math.Round(bytesPerSec*3600))) + "/h"
}
