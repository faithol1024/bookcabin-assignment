package httphandler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
	"github.com/faithol1024/bookcabin-assignment/internal/usecase/search"
	"github.com/faithol1024/bookcabin-assignment/internal/util"
)

// Limits bound what a search request may ask for.
type Limits struct {
	DefaultLimit int
	MaxLimit     int
	Passengers   Bounds
	Legs         Bounds // multi-city
}

// Bounds are inclusive.
type Bounds struct {
	Min int
	Max int
}

type searchRequest struct {
	Origin        string          `json:"origin"`
	Destination   string          `json:"destination"`
	DepartureDate string          `json:"departureDate"`
	ReturnDate    string          `json:"returnDate"`
	Legs          []legRequest    `json:"legs"`
	Passengers    int             `json:"passengers"`
	CabinClass    string          `json:"cabinClass"`
	Filters       *filtersRequest `json:"filters"`
	Sort          *sortRequest    `json:"sort"`
	Page          *pageRequest    `json:"page"`
}

type legRequest struct {
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	Date        string `json:"date"`
}

type filtersRequest struct {
	PriceMin           *int64         `json:"priceMin"`
	PriceMax           *int64         `json:"priceMax"`
	MaxStops           *int           `json:"maxStops"`
	DepartureTime      *windowRequest `json:"departureTime"`
	ArrivalTime        *windowRequest `json:"arrivalTime"`
	Airlines           []string       `json:"airlines"`
	MaxDurationMinutes *int           `json:"maxDurationMinutes"`
}

type windowRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type sortRequest struct {
	By    string `json:"by"`
	Order string `json:"order"`
}

type pageRequest struct {
	Limit  *int `json:"limit"`
	Offset int  `json:"offset"`
}

// decodeSearch reads exactly one JSON object; unknown keys fail, with a camelCase hint for snake_case.
func decodeSearch(r *http.Request) (searchRequest, error) {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req searchRequest
	err := dec.Decode(&req)
	decoded := err == nil
	if decoded {
		// a body cut off by the size cap must still report 413, even past the object
		if err = dec.Decode(&struct{}{}); err == io.EOF {
			return req, nil
		}
	}

	var (
		tooLarge *http.MaxBytesError
		typeErr  *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &tooLarge):
		return searchRequest{}, err
	case decoded:
		return searchRequest{}, invalidField("body", "must contain a single JSON object")
	case errors.As(err, &typeErr) && typeErr.Field != "":
		return searchRequest{}, invalidField(typeErr.Field, "must be "+kind(typeErr.Type))
	}
	// encoding/json has no typed error for unknown fields; TestSearch_ValidationError pins this text
	if name, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		name, _ = strconv.Unquote(name)
		msg := "unknown field"
		if strings.Contains(name, "_") {
			msg = fmt.Sprintf("unknown field; did you mean %q?", camelCase(name))
		}
		return searchRequest{}, invalidField(name, msg)
	}
	return searchRequest{}, invalidField("body", "must be a valid JSON object")
}

func invalidField(field, message string) *ValidationError {
	return &ValidationError{Errors: []FieldError{{Field: field, Message: message}}}
}

func kind(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int64:
		return "an integer"
	case reflect.String:
		return "a string"
	case reflect.Slice:
		return "an array"
	default:
		return "an object"
	}
}

func camelCase(snake string) string {
	parts := strings.Split(snake, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// criteria validates the request against the limits and reports every problem at once.
func (req searchRequest) criteria(lim Limits) (search.Criteria, error) {
	v := &ValidationError{}
	c := search.Criteria{
		Origin:        req.Origin,
		Destination:   req.Destination,
		DepartureDate: req.DepartureDate,
		ReturnDate:    req.ReturnDate,
		Passengers:    req.Passengers,
		Limit:         lim.DefaultLimit,
	}

	if req.Legs != nil {
		req.checkMultiCity(v, lim.Legs)
		for _, l := range req.Legs {
			c.Legs = append(c.Legs, search.LegQuery(l))
		}
	} else {
		req.checkTrip(v)
	}

	if req.Passengers < lim.Passengers.Min || req.Passengers > lim.Passengers.Max {
		v.add("passengers", fmt.Sprintf("must be between %d and %d", lim.Passengers.Min, lim.Passengers.Max))
	}
	cabin, ok := domain.ParseCabin(req.CabinClass)
	if !ok {
		v.add("cabinClass", "must be one of economy, business, first")
	}
	c.Cabin = cabin

	if req.Filters != nil {
		c.Filters = req.Filters.filters(v)
	}
	if req.Sort != nil {
		c.Sort = req.Sort.sort(v)
	}
	if req.Page != nil {
		if l := req.Page.Limit; l != nil {
			if *l < 1 || *l > lim.MaxLimit {
				v.add("page.limit", fmt.Sprintf("must be between 1 and %d", lim.MaxLimit))
			}
			c.Limit = *l
		}
		if req.Page.Offset < 0 {
			v.add("page.offset", "must be 0 or greater")
		}
		c.Offset = req.Page.Offset
	}

	if len(v.Errors) > 0 {
		return search.Criteria{}, v
	}
	return c, nil
}

// checkTrip validates a one-way or round-trip request.
func (req searchRequest) checkTrip(v *ValidationError) {
	checkRoute(v, "", req.Origin, req.Destination)
	dep, depOK := checkDate(v, "departureDate", req.DepartureDate)
	if req.ReturnDate == "" {
		return
	}
	if ret, ok := checkDate(v, "returnDate", req.ReturnDate); ok && depOK && ret.Before(dep) {
		v.add("returnDate", "must not be before departureDate")
	}
}

func (req searchRequest) checkMultiCity(v *ValidationError, bounds Bounds) {
	for _, f := range []struct{ name, val string }{
		{"origin", req.Origin}, {"destination", req.Destination}, {"departureDate", req.DepartureDate}, {"returnDate", req.ReturnDate},
	} {
		if f.val != "" {
			v.add(f.name, "must not be set with legs")
		}
	}
	if n := len(req.Legs); n < bounds.Min || n > bounds.Max {
		v.add("legs", fmt.Sprintf("must have between %d and %d legs", bounds.Min, bounds.Max))
	}
	var prev time.Time
	for i, l := range req.Legs {
		prefix := fmt.Sprintf("legs[%d].", i)
		checkRoute(v, prefix, l.Origin, l.Destination)
		date, ok := checkDate(v, prefix+"date", l.Date)
		if !ok {
			continue
		}
		if date.Before(prev) {
			v.add(prefix+"date", fmt.Sprintf("must not be before legs[%d].date", i-1))
		}
		prev = date
	}
}

func checkRoute(v *ValidationError, prefix, origin, destination string) {
	originOK, destinationOK := iata(origin), iata(destination)
	if !originOK {
		v.add(prefix+"origin", "must be a 3-letter upper-case IATA airport code")
	}
	if !destinationOK {
		v.add(prefix+"destination", "must be a 3-letter upper-case IATA airport code")
	}
	if originOK && destinationOK && origin == destination {
		v.add(prefix+"destination", "must differ from origin")
	}
}

func iata(code string) bool {
	return len(code) == 3 && upperAlnum(code, false)
}

// upperAlnum reports whether s is only A-Z, plus 0-9 when digits is set.
func upperAlnum(s string, digits bool) bool {
	for _, c := range s {
		if (c < 'A' || c > 'Z') && (!digits || c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func checkDate(v *ValidationError, field, s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		v.add(field, "must be a date as YYYY-MM-DD")
		return time.Time{}, false
	}
	return t, true
}

func (f filtersRequest) filters(v *ValidationError) search.Filters {
	var out search.Filters
	for _, p := range []struct {
		field string
		val   *int64
		dst   **domain.Money
	}{{"filters.priceMin", f.PriceMin, &out.PriceMin}, {"filters.priceMax", f.PriceMax, &out.PriceMax}} {
		if p.val == nil {
			continue
		}
		if *p.val < 0 {
			v.add(p.field, "must be 0 or greater")
		}
		m := domain.Money(*p.val)
		*p.dst = &m
	}
	if f.PriceMin != nil && f.PriceMax != nil && *f.PriceMin > *f.PriceMax {
		v.add("filters.priceMin", "must not exceed filters.priceMax")
	}
	if f.MaxStops != nil {
		if *f.MaxStops < 0 {
			v.add("filters.maxStops", "must be 0 or greater")
		}
		out.MaxStops = f.MaxStops
	}
	out.DepartureTime = f.DepartureTime.window(v, "filters.departureTime")
	out.ArrivalTime = f.ArrivalTime.window(v, "filters.arrivalTime")
	for i, code := range f.Airlines {
		if len(code) != 2 || !upperAlnum(code, true) {
			v.add(fmt.Sprintf("filters.airlines[%d]", i), "must be a 2-character upper-case IATA airline code")
		}
	}
	out.Airlines = f.Airlines
	if f.MaxDurationMinutes != nil {
		if *f.MaxDurationMinutes < 0 {
			v.add("filters.maxDurationMinutes", "must be 0 or greater")
		}
		d := time.Duration(*f.MaxDurationMinutes) * time.Minute
		out.MaxDuration = &d
	}
	return out
}

func (w *windowRequest) window(v *ValidationError, field string) *search.Window {
	if w == nil {
		return nil
	}
	from, errFrom := util.ParseClock(w.From)
	to, errTo := util.ParseClock(w.To)
	if errFrom != nil {
		v.add(field+".from", "must be HH:MM between 00:00 and 23:59")
	}
	if errTo != nil {
		v.add(field+".to", "must be HH:MM between 00:00 and 23:59")
	}
	return &search.Window{From: from, To: to}
}

// sort leaves an empty key or order to the service default.
func (s sortRequest) sort(v *ValidationError) search.Sort {
	out := search.Sort{By: search.SortKey(s.By), Order: search.Order(s.Order)}
	if s.By != "" && !out.By.Valid() {
		v.add("sort.by", "must be one of best_value, price, duration, departure, arrival")
	}
	if s.Order != "" && !out.Order.Valid() {
		v.add("sort.order", "must be asc or desc")
	}
	return out
}
