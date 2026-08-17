package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestParseInterspersed(t *testing.T) {
	tests := []struct {
		args     []string
		pos      []string
		interval string
		spike    bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, "", false},
		{[]string{"--interval", "5m", "a"}, []string{"a"}, "5m", false},
		{[]string{"a", "--interval", "5m", "b", "--spike"}, []string{"a", "b"}, "5m", true},
		{[]string{"a", "--interval=1h", "--", "--spike", "-x"}, []string{"a", "--spike", "-x"}, "1h", false},
		{[]string{"-", "--spike"}, []string{"-"}, "", true},
	}
	for _, tt := range tests {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		interval := fs.String("interval", "", "")
		spike := fs.Bool("spike", false, "")
		pos, err := parseInterspersed(fs, tt.args)
		if err != nil {
			t.Errorf("%v: %v", tt.args, err)
			continue
		}
		if !reflect.DeepEqual(pos, tt.pos) || *interval != tt.interval || *spike != tt.spike {
			t.Errorf("%v: pos=%q interval=%q spike=%v; want %q %q %v",
				tt.args, pos, *interval, *spike, tt.pos, tt.interval, tt.spike)
		}
	}
}

func TestPercentValue(t *testing.T) {
	var p percentValue
	for in, want := range map[string]float64{"20": 20, "20%": 20, "2.5": 2.5} {
		if err := p.Set(in); err != nil || float64(p) != want {
			t.Errorf("Set(%q) = %v, %v; want %v", in, p, err, want)
		}
	}
	for _, in := range []string{"", "0", "-5", "abc", "%"} {
		if err := p.Set(in); err == nil {
			t.Errorf("Set(%q) accepted", in)
		}
	}
}
