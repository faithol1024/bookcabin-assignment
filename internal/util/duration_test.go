package util

import (
	"math"
	"testing"
	"time"
)

func TestHoursToDuration(t *testing.T) {
	tests := []struct {
		name    string
		in      float64
		want    time.Duration
		wantErr bool
	}{
		{name: "fraction", in: 4.33, want: 259*time.Minute + 48*time.Second},
		{name: "rounds to second", in: 1.67, want: 100*time.Minute + 12*time.Second},
		{name: "whole", in: 2, want: 2 * time.Hour},
		{name: "zero", in: 0, want: 0},
		{name: "negative", in: -1, wantErr: true},
		{name: "NaN", in: math.NaN(), wantErr: true},
		{name: "infinity", in: math.Inf(1), wantErr: true},
		{name: "overflow", in: 1e12, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HoursToDuration(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseHoursMinutes(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "1h 45m", want: 105 * time.Minute},
		{in: "3h 5m", want: 185 * time.Minute},
		{in: "55m", want: 55 * time.Minute},
		{in: "3h", want: 3 * time.Hour},
		{in: "0m", want: 0},
		{in: " 1h  45m ", want: 105 * time.Minute},
		{in: "", wantErr: true},
		{in: "   ", wantErr: true},
		{in: "1h45m", wantErr: true},
		{in: "45m 1h", wantErr: true},
		{in: "1h 1h", wantErr: true},
		{in: "1h 60m", wantErr: true},
		{in: "1h 45m 3s", wantErr: true},
		{in: "-1h", wantErr: true},
		{in: "+5m", wantErr: true},
		{in: "1.5h", wantErr: true},
		{in: "h", wantErr: true},
		{in: "45", wantErr: true},
		{in: "45s", wantErr: true},
		{in: "99999h", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseHoursMinutes(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func FuzzParseHoursMinutes(f *testing.F) {
	for _, s := range []string{"1h 45m", "55m", "3h", "3h 5m", "1h 60m", "", "9999h 59m"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, err := ParseHoursMinutes(s)
		if err != nil {
			return
		}
		if got < 0 || got%time.Minute != 0 {
			t.Errorf("%q: got %v", s, got)
		}
	})
}
