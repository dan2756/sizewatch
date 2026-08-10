// Package units parses and formats byte sizes.
//
// Sizes use decimal units: 1 KB = 1000 bytes, 1 MB = 1000 KB, and so on.
package units

import "strconv"

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
