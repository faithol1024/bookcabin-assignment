package util

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	// caps each number so it can't overflow time.Duration
	maxDurationDigits = 4
	maxHours          = float64(math.MaxInt64 / int64(time.Hour))
)

// ParseHoursMinutes parses "1h 45m", "55m" or "3h".
func ParseHoursMinutes(s string) (time.Duration, error) {
	parts := strings.Fields(s)
	if len(parts) == 0 || len(parts) > 2 {
		return 0, fmt.Errorf("parse duration %q: want \"<h>h <m>m\"", s)
	}
	var d time.Duration
	hasHours := false
	for i, p := range parts {
		n, unit, err := splitUnit(p)
		if err != nil {
			return 0, fmt.Errorf("parse duration %q: %w", s, err)
		}
		switch {
		case unit == 'h' && i == 0:
			hasHours = true
			d += time.Duration(n) * time.Hour
		case unit == 'm' && i == len(parts)-1:
			if hasHours && n >= 60 {
				return 0, fmt.Errorf("parse duration %q: minutes must be below 60", s)
			}
			d += time.Duration(n) * time.Minute
		default:
			return 0, fmt.Errorf("parse duration %q: unexpected %q", s, p)
		}
	}
	return d, nil
}

func splitUnit(p string) (int, byte, error) {
	num, unit := p[:len(p)-1], p[len(p)-1]
	if num == "" || len(num) > maxDurationDigits {
		return 0, 0, fmt.Errorf("bad number in %q", p)
	}
	for i := 0; i < len(num); i++ {
		if num[i] < '0' || num[i] > '9' {
			return 0, 0, fmt.Errorf("bad number in %q", p)
		}
	}
	n, err := strconv.Atoi(num)
	if err != nil {
		return 0, 0, fmt.Errorf("bad number in %q: %w", p, err)
	}
	return n, unit, nil
}

// HoursToDuration converts float hours, rounded to the second.
func HoursToDuration(h float64) (time.Duration, error) {
	if math.IsNaN(h) || h < 0 || h > maxHours {
		return 0, fmt.Errorf("hours %v out of range", h)
	}
	return time.Duration(math.Round(h*3600)) * time.Second, nil
}
