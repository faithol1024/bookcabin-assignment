package util

import "testing"

func TestParseClock(t *testing.T) {
	for s, want := range map[string]int{"00:00": 0, "05:59": 359, "22:00": 1320, "23:59": 1439} {
		if got, err := ParseClock(s); err != nil || got != want {
			t.Errorf("ParseClock(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"", "24:00", "12:60", "7:00", "07-00", "ab:cd", "-1:00", "07:0x", "07:000"} {
		if _, err := ParseClock(s); err == nil {
			t.Errorf("ParseClock(%q) succeeded, want error", s)
		}
	}
}
