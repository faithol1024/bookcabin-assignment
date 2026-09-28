package search

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// BenchmarkSearch_10kFlights measures a leg past the gateways: conversion, validation, filters, scoring, sort and paging.
func BenchmarkSearch_10kFlights(b *testing.B) {
	rng := rand.New(rand.NewPCG(1, 2))
	jakarta, denpasar := "Jakarta", "Denpasar"
	var sources []source
	for p := range 4 {
		provider := fmt.Sprintf("P%d", p)
		flights := make([]domain.Flight, 2500)
		for i := range flights {
			f := offer(fmt.Sprintf("F%d_%s", i, provider), provider,
				price(domain.Money(300_000+rng.IntN(2_000_000))),
				departs(clock(wib, rng.IntN(24), rng.IntN(60))),
				minutesLong(60+rng.IntN(600)),
				stops(rng.IntN(3)),
				seats(rng.IntN(10)))
			f.Departure.City, f.Arrival.City = &jakarta, &denpasar
			flights[i] = f
		}
		sources = append(sources, routeSource(provider, map[string][]domain.Flight{"CGKDPS": flights}))
	}
	s := testService(sources...)
	c := oneWay(20)
	c.Filters = Filters{MaxStops: ptr(1), PriceMax: ptr[domain.Money](1_800_000)}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Search(context.Background(), c); err != nil {
			b.Fatal(err)
		}
	}
}
