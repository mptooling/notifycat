package domain

// Skip reasons for the "skipped digest" log line. They mirror the
// `ignored webhook event` reason contract: a small closed set of values an
// operator can grep for.
const (
	SkipReasonWeekend = "weekend"
	SkipReasonHoliday = "holiday"
)

// RuleKind selects how a HolidayRule resolves to a date in a given year.
type RuleKind uint8

const (
	// RuleFixed is a fixed calendar date: Month + Day.
	RuleFixed RuleKind = iota
	// RuleEasterOffset is a whole number of days from Easter Sunday
	// (negative before, positive after).
	RuleEasterOffset
	// RuleNthWeekday is the Nth Weekday of Month, where Nth == -1 means the
	// last one in the month.
	RuleNthWeekday
	// RuleWeekdayOnOrAfter is the first Weekday falling on or after Month/Day.
	// It exists for the Nordic Midsummer Eve ("the Friday on or after 19 June"),
	// which neither a fixed date nor an Nth-weekday rule can express.
	RuleWeekdayOnOrAfter
)

// ObservanceKind selects what happens when a rule's natural date lands on a
// weekend. It is only ever consulted for RuleFixed: every Easter-derived
// holiday falls on a fixed weekday, and an Nth-weekday rule names its weekday
// outright, so neither can collide with a weekend.
type ObservanceKind uint8

const (
	// ObserveNone loses the holiday when it falls on a weekend. This is the
	// real rule in Germany, France, and most of continental Europe.
	ObserveNone ObservanceKind = iota
	// ObserveUSShift moves a Saturday date to the preceding Friday and a Sunday
	// date to the following Monday — the US federal rule.
	ObserveUSShift
	// ObserveUKSubstitute moves the date forward to the first weekday not
	// already claimed by another holiday, so consecutive weekend holidays
	// produce consecutive substitute days.
	ObserveUKSubstitute
)

// CountryCode is an ISO 3166-1 alpha-2 country code selecting a holiday table.
// The empty code means no country is configured: weekends are still skipped,
// but no holiday is.
type CountryCode string

// The supported country codes. Each one has a table in holidays.go; a code
// absent from that map is rejected at startup.
const (
	CountryAustria       CountryCode = "AT"
	CountryBelgium       CountryCode = "BE"
	CountryDenmark       CountryCode = "DK"
	CountryFinland       CountryCode = "FI"
	CountryFrance        CountryCode = "FR"
	CountryGermany       CountryCode = "DE"
	CountryIreland       CountryCode = "IE"
	CountryItaly         CountryCode = "IT"
	CountryLuxembourg    CountryCode = "LU"
	CountryNetherlands   CountryCode = "NL"
	CountryNorway        CountryCode = "NO"
	CountryPoland        CountryCode = "PL"
	CountryPortugal      CountryCode = "PT"
	CountrySpain         CountryCode = "ES"
	CountrySweden        CountryCode = "SE"
	CountrySwitzerland   CountryCode = "CH"
	CountryUkraine       CountryCode = "UA"
	CountryUnitedKingdom CountryCode = "GB"
	CountryUnitedStates  CountryCode = "US"
)
