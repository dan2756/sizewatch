package units

import (
	"math"
	"testing"
	"time"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"4096", 4096},
		{"512B", 512},
		{"1KB", 1000},
		{"1kb", 1000},
		{"1K", 1000},
		{"1KiB", 1024},
		{"1kib", 1024},
		{"500MB", 500_000_000},
		{"500 MB", 500_000_000},
		{" 500mb ", 500_000_000},
		{"1.5GB", 1_500_000_000},
		{"1.5GiB", 1_610_612_736},
		{"10GB", 10_000_000_000},
		{"10G", 10_000_000_000},
		{"2MiB", 2 << 20},
		{"1TB", 1_000_000_000_000},
		{"1TiB", 1 << 40},
		{"1PB", 1e15},
		{".5KB", 500},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestParseSizeErrors(t *testing.T) {
	for _, in := range []string{"", "MB", "-5MB", "5XB", "1.2.3MB", "1e3", "5 M B", "9999999PiB", "1,000"} {
		if got, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) = %d, want error", in, got)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1000, "1.0 KB"},
		{1536, "1.5 KB"},
		{999_949, "999.9 KB"},
		{999_950, "1.0 MB"},
		{500_000_000, "500.0 MB"},
		{1_610_612_736, "1.6 GB"},
		{12_400_000_000, "12.4 GB"},
		{2_000_000_000_000, "2.0 TB"},
		{-300_000_000, "-300.0 MB"},
		{-5, "-5 B"},
		{math.MaxInt64, "9.2 EB"},
		{math.MinInt64, "-9.2 EB"},
	}
	for _, tt := range tests {
		if got := FormatSize(tt.in); got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatDelta(t *testing.T) {
	for in, want := range map[int64]string{0: "+0 B", 600_000_000: "+600.0 MB", -1500: "-1.5 KB"} {
		if got := FormatDelta(in); got != want {
			t.Errorf("FormatDelta(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestParseFormatRoundTrip(t *testing.T) {
	for _, n := range []int64{1, 999, 1_200, 45_600_000, 7_800_000_000} {
		back, err := ParseSize(FormatSize(n))
		if err != nil {
			t.Fatalf("ParseSize(FormatSize(%d)): %v", n, err)
		}
		if math.Abs(float64(back-n)) > float64(n)/100 {
			t.Errorf("%d -> %q -> %d", n, FormatSize(n), back)
		}
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"0", 0},
		{"90s", 90 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"30d", 30 * 24 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"2w3d", 17 * 24 * time.Hour},
		{"1d12h", 36 * time.Hour},
		{" 12h ", 12 * time.Hour},
		// Just under the int64 limit: 15250w + 48h overflows.
		{"15250w47h", 15250*7*24*time.Hour + 47*time.Hour},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"", "5", "d", "1.5d", "-5m", "1d-1h", "3x", "1d1w", "99999999999999w", "15250w48h", "15250w2d", "106751d24h"} {
		if got, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) = %v, want error", in, got)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		0:                       "0",
		90 * time.Second:        "1m30s",
		5 * time.Minute:         "5m",
		time.Hour:               "1h",
		90 * time.Minute:        "1h30m",
		36 * time.Hour:          "36h",
		30 * 24 * time.Hour:     "30d",
		1500 * time.Millisecond: "1.5s",
	}
	for in, want := range tests {
		if got := FormatDuration(in); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", in, got, want)
		}
		if back, err := ParseDuration(FormatDuration(in)); err != nil || back != in {
			t.Errorf("ParseDuration(FormatDuration(%v)) = %v, %v", in, back, err)
		}
	}
}

func TestParseRate(t *testing.T) {
	tests := []struct {
		in   string
		want float64 // bytes per hour, to keep the table readable
	}{
		{"1GB/h", 1e9},
		{"1GB/hour", 1e9},
		{"1 GB / h", 1e9},
		{"500MB/day", 500e6 / 24},
		{"10MiB/min", 10 * (1 << 20) * 60},
		{"1KB/s", 1000 * 3600},
		{"2GB/30m", 4e9},
		{"7GB/w", 7e9 / (7 * 24)},
		{"3GB/3h", 1e9},
	}
	for _, tt := range tests {
		got, err := ParseRate(tt.in)
		if err != nil || math.Abs(got*3600-tt.want) > 1e-6*tt.want {
			t.Errorf("ParseRate(%q) = %v B/h, %v; want %v", tt.in, got*3600, err, tt.want)
		}
	}
	for _, in := range []string{"", "1GB", "1GB/", "/h", "1GB/0s", "1GB/fortnight", "lots/h"} {
		if got, err := ParseRate(in); err == nil {
			t.Errorf("ParseRate(%q) = %v, want error", in, got)
		}
	}
}

func TestFormatRate(t *testing.T) {
	if got := FormatRate(1e9 / 3600); got != "1.0 GB/h" {
		t.Errorf("FormatRate(1GB/h) = %q", got)
	}
	if got := FormatRate(0); got != "0 B/h" {
		t.Errorf("FormatRate(0) = %q", got)
	}
}
