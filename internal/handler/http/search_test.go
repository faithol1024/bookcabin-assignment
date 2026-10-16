package httphandler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/apperr"
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
)

// fakeSearcher answers every leg of the criteria with one flight, or fails with err.
type fakeSearcher struct {
	err  error
	got  search.Criteria
	legs func(q search.LegQuery) search.Leg
}

func (f *fakeSearcher) Search(_ context.Context, c search.Criteria) (search.Response, error) {
	f.got = c
	if f.err != nil {
		return search.Response{}, f.err
	}
	queries := c.Legs
	if len(queries) == 0 {
		queries = []search.LegQuery{{Origin: c.Origin, Destination: c.Destination, Date: c.DepartureDate}}
		if c.ReturnDate != "" {
			queries = append(queries, search.LegQuery{Origin: c.Destination, Destination: c.Origin, Date: c.ReturnDate})
		}
	}
	var res search.Response
	for _, q := range queries {
		res.Legs = append(res.Legs, f.legs(q))
	}
	return res, nil
}

// Stream replays Search: each leg's flights as one batch, then its summary.
func (f *fakeSearcher) Stream(ctx context.Context, c search.Criteria, emit func(search.Event)) error {
	res, err := f.Search(ctx, c)
	for i, l := range res.Legs {
		if len(l.Flights) > 0 {
			emit(search.Event{Leg: i, Provider: l.Flights[0].Provider, Flights: l.Flights})
		}
		l.Flights = nil
		emit(search.Event{Leg: i, Summary: &l})
	}
	return err
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// usdFlight is a converted one-stop Garuda fare with no aircraft, amenities or warnings.
func usdFlight(q search.LegQuery) search.Flight {
	jakarta := "Jakarta"
	return search.Flight{
		Flight: domain.Flight{
			ID:             "GA400_GarudaIndonesia",
			Provider:       "Garuda Indonesia",
			Airline:        domain.Airline{Name: "Garuda Indonesia", Code: "GA"},
			FlightNumber:   "GA400",
			Departure:      domain.Airport{Code: q.Origin, City: &jakarta, Time: at(q.Date + "T06:00:00+07:00")},
			Arrival:        domain.Airport{Code: q.Destination, Time: at(q.Date + "T10:15:00+08:00")},
			Layovers:       []domain.Layover{{Airport: "SUB", Duration: 45 * time.Minute}},
			Price:          1064375,
			Original:       &domain.ForeignPrice{Amount: 6550, Currency: "USD", Exponent: 2, Rate: 16250},
			Cabin:          domain.CabinEconomy,
			Baggage:        domain.Baggage{CarryOn: "1 piece", Checked: "2 pieces"},
			AvailableSeats: 8,
		},
		Score: 0.1234,
		Total: 2128750,
	}
}

func oneFlightLeg(q search.LegQuery) search.Leg {
	return search.Leg{
		LegQuery: q, Flights: []search.Flight{usdFlight(q)}, TotalResults: 1,
		ProvidersQueried: 4, ProvidersSucceeded: 3, ProvidersFailed: 1, FlightsRejected: 2,
		CheapestByProvider: map[string]domain.Money{"Garuda Indonesia": 1064375},
		SearchTime:         312 * time.Millisecond,
		ProviderErrors:     []search.ProviderError{{Provider: "AirAsia", Code: search.CodeUnavailable, Attempts: 3}},
	}
}

func emptyLeg(q search.LegQuery) search.Leg {
	return search.Leg{LegQuery: q, ProvidersQueried: 4, ProvidersSucceeded: 4}
}

func newTestHandler(s Searcher) *Handler {
	return New(s, testLimits)
}

func post(t *testing.T, h *Handler, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Search(rec, httptest.NewRequest(http.MethodPost, "/v1/flights/search", strings.NewReader(body)))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return rec, out
}

func keys(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestSearch_OneWay(t *testing.T) {
	s := &fakeSearcher{legs: oneFlightLeg}
	rec, out := post(t, newTestHandler(s), withFields(`"passengers":2,"page":{"limit":5,"offset":0}`))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status %d, Content-Type %q: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if got, want := keys(out), []string{"flights", "metadata", "search_criteria"}; !reflect.DeepEqual(got, want) {
		t.Errorf("top-level keys = %v, want %v", got, want)
	}
	if s.got.Limit != 5 || s.got.Passengers != 2 {
		t.Errorf("service got limit %d, passengers %d", s.got.Limit, s.got.Passengers)
	}

	var want map[string]any
	if err := json.Unmarshal([]byte(`{
		"search_criteria": {"origin": "CGK", "destination": "DPS", "departure_date": "2025-12-15", "passengers": 2, "cabin_class": "economy"},
		"metadata": {
			"total_results": 1, "limit": 5, "offset": 0,
			"providers_queried": 4, "providers_succeeded": 3, "providers_failed": 1, "flights_rejected": 2,
			"cheapest_by_provider": {"Garuda Indonesia": 1064375},
			"search_time_ms": 312, "cache_hit": false,
			"provider_errors": [{"provider": "AirAsia", "error": "upstream_unavailable", "attempts": 3}]
		},
		"flights": [{
			"id": "GA400_GarudaIndonesia", "provider": "Garuda Indonesia",
			"airline": {"name": "Garuda Indonesia", "code": "GA"}, "flight_number": "GA400",
			"departure": {"airport": "CGK", "city": "Jakarta", "datetime": "2025-12-15T06:00:00+07:00", "timestamp": 1765753200},
			"arrival": {"airport": "DPS", "city": null, "datetime": "2025-12-15T10:15:00+08:00", "timestamp": 1765764900},
			"duration": {"total_minutes": 195, "formatted": "3h 15m"},
			"stops": 1, "layovers": [{"airport": "SUB", "duration_minutes": 45}],
			"price": {"amount": 1064375, "currency": "IDR", "formatted": "Rp 1.064.375", "total": 2128750, "total_formatted": "Rp 2.128.750",
				"original": {"amount": "65.50", "currency": "USD", "rate": 16250}},
			"available_seats": 8, "cabin_class": "economy", "aircraft": null, "amenities": [],
			"baggage": {"carry_on": "1 piece", "checked": "2 pieces"}, "score": 0.1234, "warnings": []
		}]
	}`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, want) {
		got, _ := json.MarshalIndent(out, "", "  ")
		t.Errorf("response mismatch:\n%s", got)
	}
}

func TestSearch_EmptyListsAreArrays(t *testing.T) {
	_, out := post(t, newTestHandler(&fakeSearcher{legs: emptyLeg}), oneWayBody)
	meta := out["metadata"].(map[string]any)
	for name, v := range map[string]any{"flights": out["flights"], "provider_errors": meta["provider_errors"]} {
		if a, ok := v.([]any); !ok || len(a) != 0 {
			t.Errorf("%s = %#v, want []", name, v)
		}
	}
	if m, ok := meta["cheapest_by_provider"].(map[string]any); !ok || len(m) != 0 {
		t.Errorf("cheapest_by_provider = %#v, want {}", meta["cheapest_by_provider"])
	}
}

func TestSearch_IDRFareHasNullOriginal(t *testing.T) {
	legs := func(q search.LegQuery) search.Leg {
		l := oneFlightLeg(q)
		l.Flights[0].Original = nil
		l.Flights[0].Aircraft = new("Airbus A320")
		l.Flights[0].Amenities = []string{"wifi"}
		l.Flights[0].Warnings = []domain.Warning{domain.WarningAirportUnknown}
		return l
	}
	_, out := post(t, newTestHandler(&fakeSearcher{legs: legs}), oneWayBody)
	f := out["flights"].([]any)[0].(map[string]any)
	price := f["price"].(map[string]any)
	if v, ok := price["original"]; !ok || v != nil {
		t.Errorf("price.original = %#v, want null", v)
	}
	if f["aircraft"] != "Airbus A320" || fmt.Sprint(f["amenities"]) != "[wifi]" || fmt.Sprint(f["warnings"]) != "[airport_unknown]" {
		t.Errorf("aircraft %v, amenities %v, warnings %v", f["aircraft"], f["amenities"], f["warnings"])
	}
}

func TestSearch_RoundTrip(t *testing.T) {
	legs := func(q search.LegQuery) search.Leg {
		if q.Origin == "DPS" {
			return emptyLeg(q) // no inbound flights
		}
		return oneFlightLeg(q)
	}
	rec, out := post(t, newTestHandler(&fakeSearcher{legs: legs}), withFields(`"returnDate":"2025-12-18"`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got, want := keys(out), []string{"inbound", "outbound", "search_criteria"}; !reflect.DeepEqual(got, want) {
		t.Errorf("top-level keys = %v, want %v", got, want)
	}
	if got := out["search_criteria"].(map[string]any)["return_date"]; got != "2025-12-18" {
		t.Errorf("search_criteria.return_date = %v", got)
	}
	legKeys := []string{"date", "destination", "flights", "metadata", "origin"}
	for _, name := range []string{"outbound", "inbound"} {
		if got := keys(out[name]); !reflect.DeepEqual(got, legKeys) {
			t.Errorf("%s keys = %v, want %v", name, got, legKeys)
		}
	}
	in := out["inbound"].(map[string]any)
	if in["origin"] != "DPS" || in["destination"] != "CGK" || in["date"] != "2025-12-18" || len(in["flights"].([]any)) != 0 {
		t.Errorf("inbound = %v", in)
	}
	if n := len(out["outbound"].(map[string]any)["flights"].([]any)); n != 1 {
		t.Errorf("outbound has %d flights, want 1", n)
	}
}

func TestSearch_MultiCity(t *testing.T) {
	body := multiCityBody(`{"origin":"CGK","destination":"DPS","date":"2025-12-15"},{"origin":"DPS","destination":"SUB","date":"2025-12-17"},{"origin":"SUB","destination":"CGK","date":"2025-12-20"}`)
	rec, out := post(t, newTestHandler(&fakeSearcher{legs: oneFlightLeg}), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got, want := keys(out), []string{"legs", "search_criteria"}; !reflect.DeepEqual(got, want) {
		t.Errorf("top-level keys = %v, want %v", got, want)
	}
	sc := out["search_criteria"].(map[string]any)
	if got, want := keys(sc), []string{"cabin_class", "legs", "passengers"}; !reflect.DeepEqual(got, want) {
		t.Errorf("search_criteria keys = %v, want %v", got, want)
	}
	legs := out["legs"].([]any)
	if len(legs) != 3 {
		t.Fatalf("%d legs, want 3", len(legs))
	}
	for i, want := range []string{"CGK-DPS 2025-12-15", "DPS-SUB 2025-12-17", "SUB-CGK 2025-12-20"} {
		l := legs[i].(map[string]any)
		if got := fmt.Sprintf("%s-%s %s", l["origin"], l["destination"], l["date"]); got != want {
			t.Errorf("leg %d = %s, want %s", i, got, want)
		}
	}
}

func TestSearch_ValidationError(t *testing.T) {
	s := &fakeSearcher{legs: oneFlightLeg}
	rec := httptest.NewRecorder()
	newTestHandler(s).Search(rec, httptest.NewRequest(http.MethodPost, "/v1/flights/search", strings.NewReader(withFields(`"departure_date":"2025-12-15"`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	p := decodeProblem(t, rec)
	want := []FieldError{{Field: "departure_date", Message: `unknown field; did you mean "departureDate"?`}}
	if p.Type != "validation_error" || !reflect.DeepEqual(p.Errors, want) {
		t.Errorf("problem = %+v", p)
	}
	if s.got.Cabin != "" {
		t.Error("service called for an invalid request")
	}
}

func TestSearch_AllProvidersFailed(t *testing.T) {
	rec := httptest.NewRecorder()
	h := newTestHandler(&fakeSearcher{err: fmt.Errorf("search 1 legs: %w", apperr.ErrAllProvidersFailed)})
	h.Search(rec, httptest.NewRequest(http.MethodPost, "/v1/flights/search", strings.NewReader(oneWayBody)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Type != "all_providers_failed" {
		t.Errorf("problem = %+v", p)
	}
}

func TestDecimal(t *testing.T) {
	tests := []struct {
		minor int64
		exp   int
		want  string
	}{
		{6550, 2, "65.50"}, {5, 2, "0.05"}, {50, 2, "0.50"}, {100, 2, "1.00"}, {12000, 0, "12000"}, {-150, 2, "-1.50"}, {7, 3, "0.007"},
	}
	for _, tt := range tests {
		if got := decimal(tt.minor, tt.exp); got != tt.want {
			t.Errorf("decimal(%d, %d) = %q, want %q", tt.minor, tt.exp, got, tt.want)
		}
	}
}

func TestSearch_OversizeTrailingDataIs413(t *testing.T) {
	const limit = 64 << 10
	h := http.MaxBytesHandler(http.HandlerFunc(newTestHandler(&fakeSearcher{legs: oneFlightLeg}).Search), limit)
	body := oneWayBody + strings.Repeat(" ", limit)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/flights/search", strings.NewReader(body)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body)
	}
	if p := decodeProblem(t, rec); p.Type != "payload_too_large" {
		t.Errorf("problem = %+v", p)
	}
}
