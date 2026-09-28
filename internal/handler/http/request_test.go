package httphandler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
)

// testLimits mirror the committed flight_search.api values.
var testLimits = Limits{DefaultLimit: 20, MaxLimit: 100, Passengers: Bounds{Min: 1, Max: 9}, Legs: Bounds{Min: 2, Max: 5}}

const oneWayBody = `{"origin":"CGK","destination":"DPS","departureDate":"2025-12-15","passengers":1,"cabinClass":"economy"}`

// withFields merges extra top-level JSON members into the one-way body.
func withFields(extra string) string {
	return strings.TrimSuffix(oneWayBody, "}") + "," + extra + "}"
}

func multiCityBody(legs string) string {
	return `{"legs":[` + legs + `],"passengers":1,"cabinClass":"economy"}`
}

func parse(body string) (search.Criteria, error) {
	req, err := decodeSearch(httptest.NewRequest(http.MethodPost, "/v1/flights/search", strings.NewReader(body)))
	if err != nil {
		return search.Criteria{}, err
	}
	return req.criteria(testLimits)
}

func fieldErrors(t *testing.T, err error) []FieldError {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("error = %v, want a *ValidationError", err)
	}
	return v.Errors
}

func TestCriteria_Valid(t *testing.T) {
	ptr := func(m domain.Money) *domain.Money { return &m }
	stops, dur := 0, 240*time.Minute
	tests := []struct {
		name string
		body string
		want search.Criteria
	}{
		{"one-way defaults", oneWayBody, search.Criteria{
			Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", Passengers: 1, Cabin: domain.CabinEconomy, Limit: 20,
		}},
		{"assignment sample", `{"origin":"CGK","destination":"DPS","departureDate":"2025-12-15","returnDate":null,"legs":null,
			"passengers":2,"cabinClass":"business",
			"filters":{"priceMin":500000,"priceMax":1500000,"maxStops":0,"departureTime":{"from":"22:00","to":"02:00"},
				"arrivalTime":{"from":"00:00","to":"23:59"},"airlines":["GA","JT"],"maxDurationMinutes":240},
			"sort":{"by":"price","order":"desc"},"page":{"limit":100,"offset":5}}`, search.Criteria{
			Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", Passengers: 2, Cabin: domain.CabinBusiness,
			Filters: search.Filters{
				PriceMin: ptr(500000), PriceMax: ptr(1500000), MaxStops: &stops,
				DepartureTime: &search.Window{From: 22 * 60, To: 2 * 60}, ArrivalTime: &search.Window{From: 0, To: 23*60 + 59},
				Airlines: []string{"GA", "JT"}, MaxDuration: &dur,
			},
			Sort: search.Sort{By: search.SortPrice, Order: search.Desc}, Limit: 100, Offset: 5,
		}},
		{"round trip on the same day", withFields(`"returnDate":"2025-12-15"`), search.Criteria{
			Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", ReturnDate: "2025-12-15", Passengers: 1, Cabin: domain.CabinEconomy, Limit: 20,
		}},
		{"multi-city with a repeated date", multiCityBody(`{"origin":"CGK","destination":"DPS","date":"2025-12-15"},{"origin":"DPS","destination":"SUB","date":"2025-12-15"}`), search.Criteria{
			Legs:       []search.LegQuery{{Origin: "CGK", Destination: "DPS", Date: "2025-12-15"}, {Origin: "DPS", Destination: "SUB", Date: "2025-12-15"}},
			Passengers: 1, Cabin: domain.CabinEconomy, Limit: 20,
		}},
		{"bounds are inclusive", withFields(`"page":{"limit":1,"offset":0}`), search.Criteria{
			Origin: "CGK", Destination: "DPS", DepartureDate: "2025-12-15", Passengers: 1, Cabin: domain.CabinEconomy, Limit: 1,
		}},
		{"past date allowed", strings.Replace(oneWayBody, "2025-12-15", "2001-01-01", 1), search.Criteria{
			Origin: "CGK", Destination: "DPS", DepartureDate: "2001-01-01", Passengers: 1, Cabin: domain.CabinEconomy, Limit: 20,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.body)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("criteria\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestCriteria_Invalid(t *testing.T) {
	leg := func(o, d, date string) string {
		return fmt.Sprintf(`{"origin":%q,"destination":%q,"date":%q}`, o, d, date)
	}
	twoLegs := leg("CGK", "DPS", "2025-12-15") + "," + leg("DPS", "SUB", "2025-12-16")
	tests := []struct {
		name  string
		body  string
		field string
		msg   string // substring
	}{
		{"malformed json", `{"origin":`, "body", "valid JSON object"},
		{"empty body", ``, "body", "valid JSON object"},
		{"not an object", `[]`, "body", "valid JSON object"},
		{"two objects", oneWayBody + oneWayBody, "body", "single JSON object"},
		{"wrong type", withFields(`"page":{"limit":"ten"}`), "page.limit", "must be an integer"},
		{"fractional passengers", strings.Replace(oneWayBody, `"passengers":1`, `"passengers":1.5`, 1), "passengers", "must be an integer"},
		{"unknown field", withFields(`"currency":"IDR"`), "currency", "unknown field"},
		{"snake_case field", withFields(`"departure_date":"2025-12-15"`), "departure_date", `did you mean "departureDate"?`},
		{"nested snake_case field", withFields(`"filters":{"max_duration_minutes":60}`), "max_duration_minutes", `did you mean "maxDurationMinutes"?`},
		{"lower-case origin", strings.Replace(oneWayBody, "CGK", "cgk", 1), "origin", "IATA airport code"},
		{"long destination", strings.Replace(oneWayBody, "DPS", "DPSX", 1), "destination", "IATA airport code"},
		{"missing origin", strings.Replace(oneWayBody, `"origin":"CGK",`, "", 1), "origin", "IATA airport code"},
		{"origin equals destination", strings.Replace(oneWayBody, "DPS", "CGK", 1), "destination", "must differ from origin"},
		{"bad departure date", strings.Replace(oneWayBody, "2025-12-15", "15-12-2025", 1), "departureDate", "YYYY-MM-DD"},
		{"impossible date", strings.Replace(oneWayBody, "2025-12-15", "2025-02-30", 1), "departureDate", "YYYY-MM-DD"},
		{"bad return date", withFields(`"returnDate":"tomorrow"`), "returnDate", "YYYY-MM-DD"},
		{"return before departure", withFields(`"returnDate":"2025-12-14"`), "returnDate", "must not be before departureDate"},
		{"return date with legs", `{"legs":[` + twoLegs + `],"returnDate":"2025-12-20","passengers":1,"cabinClass":"economy"}`, "returnDate", "must not be set with legs"},
		{"origin with legs", `{"origin":"CGK","legs":[` + twoLegs + `],"passengers":1,"cabinClass":"economy"}`, "origin", "must not be set with legs"},
		{"destination with legs", `{"destination":"DPS","legs":[` + twoLegs + `],"passengers":1,"cabinClass":"economy"}`, "destination", "must not be set with legs"},
		{"departure date with legs", `{"departureDate":"2025-12-15","legs":[` + twoLegs + `],"passengers":1,"cabinClass":"economy"}`, "departureDate", "must not be set with legs"},
		{"empty legs", multiCityBody(""), "legs", "between 2 and 5 legs"},
		{"one leg", multiCityBody(leg("CGK", "DPS", "2025-12-15")), "legs", "between 2 and 5 legs"},
		{"six legs", multiCityBody(strings.Repeat(leg("CGK", "DPS", "2025-12-15")+",", 5) + leg("CGK", "DPS", "2025-12-15")), "legs", "between 2 and 5 legs"},
		{"decreasing leg dates", multiCityBody(leg("CGK", "DPS", "2025-12-15") + "," + leg("DPS", "SUB", "2025-12-14")), "legs[1].date", "must not be before legs[0].date"},
		{"bad leg airport", multiCityBody(leg("CGK", "dps", "2025-12-15") + "," + leg("DPS", "SUB", "2025-12-16")), "legs[0].destination", "IATA airport code"},
		{"leg origin equals destination", multiCityBody(leg("CGK", "DPS", "2025-12-15") + "," + leg("SUB", "SUB", "2025-12-16")), "legs[1].destination", "must differ from origin"},
		{"bad leg date", multiCityBody(leg("CGK", "DPS", "2025/12/15") + "," + leg("DPS", "SUB", "2025-12-16")), "legs[0].date", "YYYY-MM-DD"},
		{"zero passengers", strings.Replace(oneWayBody, `"passengers":1`, `"passengers":0`, 1), "passengers", "between 1 and 9"},
		{"ten passengers", strings.Replace(oneWayBody, `"passengers":1`, `"passengers":10`, 1), "passengers", "between 1 and 9"},
		{"missing passengers", strings.Replace(oneWayBody, `"passengers":1,`, "", 1), "passengers", "between 1 and 9"},
		{"unknown cabin", strings.Replace(oneWayBody, "economy", "premium", 1), "cabinClass", "economy, business, first"},
		{"upper-case cabin", strings.Replace(oneWayBody, "economy", "ECONOMY", 1), "cabinClass", "economy, business, first"},
		{"price min above max", withFields(`"filters":{"priceMin":2000000,"priceMax":1000000}`), "filters.priceMin", "must not exceed filters.priceMax"},
		{"negative price", withFields(`"filters":{"priceMax":-1}`), "filters.priceMax", "0 or greater"},
		{"negative max stops", withFields(`"filters":{"maxStops":-1}`), "filters.maxStops", "0 or greater"},
		{"negative max duration", withFields(`"filters":{"maxDurationMinutes":-1}`), "filters.maxDurationMinutes", "0 or greater"},
		{"bad departure window", withFields(`"filters":{"departureTime":{"from":"6:00","to":"12:00"}}`), "filters.departureTime.from", "HH:MM"},
		{"arrival window past midnight", withFields(`"filters":{"arrivalTime":{"from":"00:00","to":"24:00"}}`), "filters.arrivalTime.to", "HH:MM"},
		{"window missing to", withFields(`"filters":{"arrivalTime":{"from":"00:00"}}`), "filters.arrivalTime.to", "HH:MM"},
		{"bad airline code", withFields(`"filters":{"airlines":["GA","garuda"]}`), "filters.airlines[1]", "IATA airline code"},
		{"unknown sort key", withFields(`"sort":{"by":"cheapest"}`), "sort.by", "best_value, price, duration, departure, arrival"},
		{"unknown sort order", withFields(`"sort":{"by":"price","order":"up"}`), "sort.order", "asc or desc"},
		{"zero limit", withFields(`"page":{"limit":0}`), "page.limit", "between 1 and 100"},
		{"limit above max", withFields(`"page":{"limit":101}`), "page.limit", "between 1 and 100"},
		{"negative offset", withFields(`"page":{"offset":-1}`), "page.offset", "0 or greater"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(tt.body)
			errs := fieldErrors(t, err)
			for _, e := range errs {
				if e.Field == tt.field && strings.Contains(e.Message, tt.msg) {
					return
				}
			}
			t.Errorf("errors = %+v, want %s: ...%s...", errs, tt.field, tt.msg)
		})
	}
}

func TestCriteria_ReportsEveryProblem(t *testing.T) {
	_, err := parse(`{"origin":"cgk","destination":"DPS","departureDate":"x","passengers":0,"cabinClass":"coach","page":{"offset":-1}}`)
	var got []string
	for _, e := range fieldErrors(t, err) {
		got = append(got, e.Field)
	}
	want := []string{"origin", "departureDate", "passengers", "cabinClass", "page.offset"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fields = %v, want %v", got, want)
	}
}

func TestDecodeSearch_BodyTooLarge(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(withFields(`"x":"`+strings.Repeat("a", 100)+`"`)))
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 64)
	_, err := decodeSearch(r)
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		t.Errorf("error = %v, want *http.MaxBytesError", err)
	}
}

func TestCamelCase(t *testing.T) {
	for in, want := range map[string]string{
		"departure_date": "departureDate", "max_duration_minutes": "maxDurationMinutes", "origin": "origin", "trailing_": "trailing",
	} {
		if got := camelCase(in); got != want {
			t.Errorf("camelCase(%q) = %q, want %q", in, got, want)
		}
	}
}
