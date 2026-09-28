package domain

import (
	"math"
	"testing"
)

func TestFormatIDR(t *testing.T) {
	tests := []struct {
		in   Money
		want string
	}{
		{0, "Rp 0"},
		{999, "Rp 999"},
		{1000, "Rp 1.000"},
		{780000, "Rp 780.000"},
		{1250000, "Rp 1.250.000"},
		{1234567890, "Rp 1.234.567.890"},
		{-1000, "-Rp 1.000"},
		{math.MaxInt64, "Rp 9.223.372.036.854.775.807"},
		{math.MinInt64, "-Rp 9.223.372.036.854.775.808"},
	}
	for _, tt := range tests {
		if got := FormatIDR(tt.in); got != tt.want {
			t.Errorf("FormatIDR(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0m"},
		{55, "55m"},
		{60, "1h 0m"},
		{230, "3h 50m"},
		{260, "4h 20m"},
	}
	for _, tt := range tests {
		if got := FormatDuration(tt.in); got != tt.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
