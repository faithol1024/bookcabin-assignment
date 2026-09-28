package search

import (
	"github.com/faithol1024/bookcabin-assignment/internal/domain"
)

// cities fills the city of airports the provider left out; can be moved to a database or an airport API.
var cities = map[string]string{
	"AMQ": "Ambon",
	"BDJ": "Banjarmasin",
	"BDO": "Bandung",
	"BPN": "Balikpapan",
	"BTH": "Batam",
	"BTJ": "Banda Aceh",
	"CGK": "Jakarta",
	"DJB": "Jambi",
	"DJJ": "Jayapura",
	"DPS": "Denpasar",
	"HLP": "Jakarta",
	"JOG": "Yogyakarta",
	"KNO": "Medan",
	"KOE": "Kupang",
	"LBJ": "Labuan Bajo",
	"LOP": "Lombok",
	"MDC": "Manado",
	"MLG": "Malang",
	"PDG": "Padang",
	"PKU": "Pekanbaru",
	"PLM": "Palembang",
	"PNK": "Pontianak",
	"SOC": "Solo",
	"SOQ": "Sorong",
	"SRG": "Semarang",
	"SUB": "Surabaya",
	"TIM": "Timika",
	"TKG": "Bandar Lampung",
	"UPG": "Makassar",
	"YIA": "Yogyakarta",
}

// fillCities sets every airport still missing a city from the table; an airport not in it keeps a nil
// city and adds airport_unknown, never a rejection.
func fillCities(flights []domain.Flight) {
	for i := range flights {
		f := &flights[i]
		unknown := false
		for _, a := range []*domain.Airport{&f.Departure, &f.Arrival} {
			if a.City != nil {
				continue
			}
			if c, ok := cities[a.Code]; ok {
				a.City = &c
			} else {
				unknown = true
			}
		}
		if unknown {
			f.Warnings = append(f.Warnings, domain.WarningAirportUnknown)
		}
	}
}
