package util

import (
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // tests must not depend on the host zoneinfo
)

func TestParseZonedTime(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		zone    string
		want    string
		wantErr bool
	}{
		{name: "WIB", in: "2025-12-15T05:30:00", zone: "Asia/Jakarta", want: "2025-12-15T05:30:00+07:00"},
		{name: "WITA", in: "2025-12-15T08:15:00", zone: "Asia/Makassar", want: "2025-12-15T08:15:00+08:00"},
		{name: "WIT", in: "2025-12-15T10:00:00", zone: "Asia/Jayapura", want: "2025-12-15T10:00:00+09:00"},
		{name: "no zone", in: "2025-12-15T05:30:00", zone: "", wantErr: true},
		{name: "host zone", in: "2025-12-15T05:30:00", zone: "Local", wantErr: true},
		{name: "unknown zone", in: "2025-12-15T05:30:00", zone: "Asia/Atlantis", wantErr: true},
		{name: "offset in value", in: "2025-12-15T05:30:00+07:00", zone: "Asia/Jakarta", wantErr: true},
		{name: "bad time", in: "2025-12-15 05:30", zone: "Asia/Jakarta", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseZonedTime(tt.in, tt.zone)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s := got.Format(time.RFC3339); s != tt.want {
				t.Errorf("got %s, want %s", s, tt.want)
			}
		})
	}
}

func TestParseZonedTime_CachesLocationConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := ParseZonedTime("2025-12-15T05:30:00", "Asia/Makassar"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	a, _ := location("Asia/Makassar")
	b, _ := location("Asia/Makassar")
	if a != b {
		t.Error("location not cached")
	}
}
