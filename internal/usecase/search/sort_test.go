package search

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

func ranked(flights ...domain.Flight) []Flight {
	out := make([]Flight, len(flights))
	for i, f := range flights {
		out[i] = Flight{Flight: f}
	}
	return out
}

func rankedIDs(flights []Flight) []string {
	ids := make([]string, 0, len(flights))
	for _, f := range flights {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestSortFlights_EachKey(t *testing.T) {
	// a < b < c on every key
	a := Flight{Flight: flight("a", price(100), departs(clock(wib, 6, 0)), minutesLong(60)), Score: 0.1}
	b := Flight{Flight: flight("b", price(200), departs(clock(wib, 7, 0)), minutesLong(90)), Score: 0.2}
	c := Flight{Flight: flight("c", price(300), departs(clock(wib, 8, 0)), minutesLong(120)), Score: 0.3}
	for _, key := range []SortKey{SortBestValue, SortPrice, SortDuration, SortDeparture, SortArrival} {
		for order, want := range map[Order][]string{Asc: {"a", "b", "c"}, Desc: {"c", "b", "a"}} {
			t.Run(fmt.Sprintf("%s %s", key, order), func(t *testing.T) {
				flights := []Flight{b, c, a}
				sortFlights(flights, Sort{By: key, Order: order})
				if got := rankedIDs(flights); !slices.Equal(got, want) {
					t.Errorf("order %v, want %v", got, want)
				}
			})
		}
	}
}

func TestSortFlights_IDBreaksTiesInBothOrders(t *testing.T) {
	flights := ranked(flight("ID7042_BatikAir"), flight("GA400_GarudaIndonesia", price(1)), flight("JT650_LionAir"), flight("GA410_GarudaIndonesia"))
	sortFlights(flights, Sort{By: SortPrice, Order: Asc})
	if want := []string{"GA400_GarudaIndonesia", "GA410_GarudaIndonesia", "ID7042_BatikAir", "JT650_LionAir"}; !slices.Equal(rankedIDs(flights), want) {
		t.Errorf("asc %v, want %v", rankedIDs(flights), want)
	}
	sortFlights(flights, Sort{By: SortPrice, Order: Desc})
	if want := []string{"GA410_GarudaIndonesia", "ID7042_BatikAir", "JT650_LionAir", "GA400_GarudaIndonesia"}; !slices.Equal(rankedIDs(flights), want) {
		t.Errorf("desc %v, want %v", rankedIDs(flights), want)
	}
}

func TestSort_Defaults(t *testing.T) {
	if got := (Sort{}).withDefaults(); got != (Sort{By: SortBestValue, Order: Asc}) {
		t.Errorf("zero Sort = %+v, want best_value asc", got)
	}
	if got := (Sort{Order: Desc}).withDefaults(); got != (Sort{By: SortBestValue, Order: Desc}) {
		t.Errorf("desc only = %+v, want best_value desc", got)
	}
	if got := (Sort{By: SortPrice}).withDefaults(); got != (Sort{By: SortPrice, Order: Asc}) {
		t.Errorf("price only = %+v, want price asc", got)
	}
}

func TestSort_Valid(t *testing.T) {
	for _, k := range []SortKey{"cheapest", "", "Price"} {
		if k.Valid() {
			t.Errorf("key %q valid, want invalid", k)
		}
	}
	for _, o := range []Order{"up", "", "ASC"} {
		if o.Valid() {
			t.Errorf("order %q valid, want invalid", o)
		}
	}
	for _, k := range []SortKey{SortBestValue, SortPrice, SortDuration, SortDeparture, SortArrival} {
		if !k.Valid() {
			t.Errorf("key %q invalid", k)
		}
	}
	if !Asc.Valid() || !Desc.Valid() {
		t.Error("asc/desc invalid")
	}
}

func TestSortFlights_PermutationProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for run := range 200 {
		n := rng.IntN(50)
		in := make([]Flight, n)
		for i := range in {
			// small ranges force ties
			in[i] = Flight{
				Flight: flight(fmt.Sprintf("F%03d", rng.IntN(1000)),
					price(domain.Money(rng.IntN(5)*100_000)),
					departs(clock(wib, rng.IntN(24), rng.IntN(4)*15)),
					minutesLong(60+rng.IntN(4)*30)),
				Score: float64(rng.IntN(5)) / 4,
			}
		}
		s := Sort{By: []SortKey{SortBestValue, SortPrice, SortDuration, SortDeparture, SortArrival}[rng.IntN(5)], Order: []Order{Asc, Desc}[rng.IntN(2)]}
		out := slices.Clone(in)
		sortFlights(out, s)

		wantSet, gotSet := slices.SortedFunc(slices.Values(in), compareFlight), slices.SortedFunc(slices.Values(out), compareFlight)
		if !slices.EqualFunc(wantSet, gotSet, func(a, b Flight) bool { return compareFlight(a, b) == 0 }) {
			t.Fatalf("run %d (%+v): output is not a permutation of the input", run, s)
		}
		for i := 1; i < len(out); i++ {
			c := keys[s.By](out[i-1], out[i])
			if s.Order == Desc {
				c = -c
			}
			if c > 0 || (c == 0 && out[i-1].ID > out[i].ID) {
				t.Fatalf("run %d (%+v): %s before %s out of order", run, s, out[i-1].ID, out[i].ID)
			}
		}
	}
}

// compareFlight orders by every field the property generates, so equal means the same flight.
func compareFlight(a, b Flight) int {
	if c := strings.Compare(a.ID, b.ID); c != 0 {
		return c
	}
	for _, c := range []int{
		int(a.Price - b.Price),
		a.Departure.Time.Compare(b.Departure.Time),
		int((a.Duration() - b.Duration()) / time.Minute),
		int((a.Score - b.Score) * 4),
	} {
		if c != 0 {
			return c
		}
	}
	return 0
}
