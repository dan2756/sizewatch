// Package units parses and formats byte sizes.
//
// Parsing accepts both bases: KB, MB, GB, TB and PB are decimal (powers of
// 1000) and KiB, MiB, GiB, TiB and PiB are binary (powers of 1024).
// Formatting always uses decimal units, so 1 GB = 1,000,000,000 bytes.
package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
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
