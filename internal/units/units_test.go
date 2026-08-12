package units

import (
	"math"
	"testing"
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
