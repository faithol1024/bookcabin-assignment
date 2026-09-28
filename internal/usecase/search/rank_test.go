package search

import (
	"math"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// committedScore mirrors flight_search.search.score in the committed config files.
var committedScore = Score{
	Weights:      Weights{Price: 0.5, Duration: 0.3, Stops: 0.15, Redeye: 0.05},
	MaxStops:     2,
	RedeyeWindow: Window{From: 0, To: 5*60 + 59},
}

func scores(ranked []Flight) map[string]float64 {
	out := make(map[string]float64, len(ranked))
	for _, f := range ranked {
		out[f.ID] = f.Score
	}
	return out
}

func assertScores(t *testing.T, got []Flight, want map[string]float64) {
	t.Helper()
	s := scores(got)
	if len(s) != len(want) {
		t.Fatalf("scored %v, want %v", s, want)
	}
	for id, w := range want {
		if math.IsNaN(s[id]) || math.Abs(s[id]-w) > 1e-9 {
			t.Errorf("%s score = %v, want %v", id, s[id], w)
		}
	}
}

func TestRank_Formula(t *testing.T) {
	flights := []domain.Flight{
		flight("cheap-slow", price(500_000), minutesLong(300), stops(1)),
		flight("dear-fast", price(1_500_000), minutesLong(100)),
		flight("mid-redeye", price(1_000_000), departs(clock(wib, 5, 0)), minutesLong(200), stops(3)),
	}
	// mid-redeye: price 0.5, duration 0.5, stops capped at 2 → 1, red-eye 1
	assertScores(t, committedScore.rank(flights, 1), map[string]float64{
		"cheap-slow": 0.5*0 + 0.3*1 + 0.15*0.5,
		"dear-fast":  0.5 * 1,
		"mid-redeye": 0.5*0.5 + 0.3*0.5 + 0.15*1 + 0.05*1,
	})
}

func TestRank_SingleFlight(t *testing.T) {
	assertScores(t, committedScore.rank([]domain.Flight{flight("only", stops(1))}, 1), map[string]float64{"only": 0.15 * 0.5})
}

func TestRank_AllEqualPrices(t *testing.T) {
	flights := []domain.Flight{flight("a", minutesLong(120)), flight("b", minutesLong(180)), flight("c", minutesLong(240))}
	assertScores(t, committedScore.rank(flights, 1), map[string]float64{"a": 0, "b": 0.15, "c": 0.3})
}

func TestRank_AllEqualFlights(t *testing.T) {
	assertScores(t, committedScore.rank([]domain.Flight{flight("a"), flight("b")}, 1), map[string]float64{"a": 0, "b": 0})
}

func TestRank_RedeyeBoundary(t *testing.T) {
	flights := []domain.Flight{
		flight("05:59", departs(clock(wib, 5, 59)), minutesLong(120)),
		flight("06:00", departs(clock(wib, 6, 0)), minutesLong(120)),
	}
	assertScores(t, committedScore.rank(flights, 1), map[string]float64{"05:59": 0.05, "06:00": 0})
}

func TestRank_WeightsNormalizedAndRounded(t *testing.T) {
	s := Score{Weights: Weights{Price: 2}, MaxStops: 2}
	flights := []domain.Flight{flight("a", price(0)), flight("b", price(1)), flight("c", price(3))}
	assertScores(t, s.rank(flights, 1), map[string]float64{"a": 0, "b": 0.3333, "c": 1})
}

func TestRank_TotalForAllPassengers(t *testing.T) {
	got := committedScore.rank([]domain.Flight{flight("a", price(780_000))}, 3)
	if got[0].Price != 780_000 || got[0].Total != 2_340_000 {
		t.Errorf("price %d, total %d; want 780000, 2340000", got[0].Price, got[0].Total)
	}
}

func TestRank_Empty(t *testing.T) {
	if got := committedScore.rank(nil, 1); len(got) != 0 {
		t.Errorf("rank(nil) = %v", got)
	}
}
