package util

import (
	"fmt"
	"sync"
	"time"
)

const localTimeLayout = "2006-01-02T15:04:05"

var locations = struct {
	sync.Mutex
	m map[string]*time.Location
}{m: map[string]*time.Location{}}

// ParseZonedTime parses a naive local time in an IANA zone, e.g. 2025-12-15T05:30:00 in Asia/Jakarta.
func ParseZonedTime(s, zone string) (time.Time, error) {
	loc, err := location(zone)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	t, err := time.ParseInLocation(localTimeLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q in %s: %w", s, zone, err)
	}
	return t, nil
}

func location(zone string) (*time.Location, error) {
	// "" loads UTC and "Local" the host zone; neither is an explicit zone
	if zone == "" || zone == "Local" {
		return nil, fmt.Errorf("unresolvable zone %q", zone)
	}
	locations.Lock()
	defer locations.Unlock()
	if loc, ok := locations.m[zone]; ok {
		return loc, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("load zone %q: %w", zone, err)
	}
	locations.m[zone] = loc
	return loc, nil
}
