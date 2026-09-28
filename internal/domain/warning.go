package domain

type Warning string

const (
	WarningDurationMismatch    Warning = "duration_mismatch"
	WarningSegmentInconsistent Warning = "segment_inconsistent"
	WarningAirportUnknown      Warning = "airport_unknown"
	WarningBaggageUnparsed     Warning = "baggage_unparsed"
)
