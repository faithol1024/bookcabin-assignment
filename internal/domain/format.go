package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatIDR renders Rp 1.250.000.
func FormatIDR(m Money) string {
	u := uint64(m)
	var b strings.Builder
	if m < 0 {
		b.WriteByte('-')
		u = -u // unsigned negation stays correct for MinInt64
	}
	b.WriteString("Rp ")
	digits := strconv.FormatUint(u, 10)
	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteByte(digits[i])
	}
	return b.String()
}

// FormatDuration renders 4h 20m, 1h 0m or 55m.
func FormatDuration(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dh %dm", minutes/60, minutes%60)
}
