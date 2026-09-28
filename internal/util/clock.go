package util

import (
	"fmt"
	"strconv"
)

// ParseClock reads HH:MM (00:00 to 23:59) as minutes after midnight.
func ParseClock(s string) (int, error) {
	if len(s) != 5 || s[2] != ':' {
		return 0, fmt.Errorf("time %q must be HH:MM", s)
	}
	h, errH := strconv.Atoi(s[:2])
	m, errM := strconv.Atoi(s[3:])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("time %q must be HH:MM between 00:00 and 23:59", s)
	}
	return h*60 + m, nil
}
