package application

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
)

func calendarLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func newTestCalendar(t *testing.T, country string) *Calendar {
	t.Helper()
	return NewCalendar(domain.CalendarParams{Country: country, Logger: calendarLogger(&bytes.Buffer{})})
}

func date(t *testing.T, value string, tz *time.Location) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", value, tz)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

// An unrecognized country must degrade to weekends-only with a warning, never
// abort startup: the digest is one feature and the code is a cosmetic setting,
// so a typo must not take the whole server down.
func TestNewCalendar_WarnsAndIgnoresUnknownCountry(t *testing.T) {
	var buf bytes.Buffer
	calendar := NewCalendar(domain.CalendarParams{Country: "ZZ", Logger: calendarLogger(&buf)})

	logged := buf.String()
	if !strings.Contains(logged, "digest country not recognized") {
		t.Errorf("no warning for an unknown country: %q", logged)
	}
	if !strings.Contains(logged, "ZZ") {
		t.Errorf("warning does not name the offending code: %q", logged)
	}
	if !strings.Contains(logged, "DE") {
		t.Errorf("warning does not list the supported codes: %q", logged)
	}

	// Weekends still skipped...
	if reason, skip := calendar.SkipReason(date(t, "2026-07-04", time.UTC)); !skip || reason != domain.SkipReasonWeekend {
		t.Errorf("Saturday: reason=%q skip=%v; want %q true", reason, skip, domain.SkipReasonWeekend)
	}
	// ...but no holiday is, exactly as if no country were set.
	if reason, skip := calendar.SkipReason(date(t, "2026-12-25", time.UTC)); skip {
		t.Errorf("2026-12-25 skipped as %q; an unknown country must skip no holidays", reason)
	}
}

func TestNewCalendar_UnknownCountryWarnsOnlyOnce(t *testing.T) {
	var buf bytes.Buffer
	calendar := NewCalendar(domain.CalendarParams{Country: "ZZ", Logger: calendarLogger(&buf)})
	if got := strings.Count(buf.String(), "digest country not recognized"); got != 1 {
		t.Fatalf("warning logged %d times at construction; want exactly 1", got)
	}
	buf.Reset()
	calendar.SkipReason(date(t, "2026-12-25", time.UTC))
	if buf.Len() != 0 {
		t.Fatalf("SkipReason logged on a tick; want silence, got %q", buf.String())
	}
}

func TestNewCalendar_NormalizesCountryCase(t *testing.T) {
	calendar := newTestCalendar(t, "de")
	reason, skip := calendar.SkipReason(date(t, "2026-12-25", time.UTC))
	if !skip || reason != domain.SkipReasonHoliday {
		t.Fatalf("lowercase country not honored: reason=%q skip=%v; want %q true", reason, skip, domain.SkipReasonHoliday)
	}
}

func TestNewCalendar_WarnsOnceWhenCountryUnset(t *testing.T) {
	var buf bytes.Buffer
	calendar := NewCalendar(domain.CalendarParams{Logger: calendarLogger(&buf)})
	if got := strings.Count(buf.String(), "digest holidays not configured"); got != 1 {
		t.Fatalf("warning logged %d times at construction; want exactly 1", got)
	}
	buf.Reset()
	calendar.SkipReason(date(t, "2026-12-25", time.UTC))
	calendar.SkipReason(date(t, "2026-12-26", time.UTC))
	if buf.Len() != 0 {
		t.Fatalf("SkipReason logged on a tick; want silence, got %q", buf.String())
	}
}

func TestNewCalendar_DoesNotWarnWhenCountrySet(t *testing.T) {
	var buf bytes.Buffer
	NewCalendar(domain.CalendarParams{Country: "DE", Logger: calendarLogger(&buf)})
	if strings.Contains(buf.String(), "digest holidays not configured") {
		t.Fatalf("warned despite a configured country: %q", buf.String())
	}
}

func TestSkipReason_Weekend(t *testing.T) {
	cases := []struct {
		name    string
		country string
		day     string
	}{
		{"saturday with country", "DE", "2026-07-04"},
		{"sunday with country", "DE", "2026-07-05"},
		{"saturday without country", "", "2026-07-04"},
		{"sunday without country", "", "2026-07-05"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			calendar := newTestCalendar(t, testCase.country)
			reason, skip := calendar.SkipReason(date(t, testCase.day, time.UTC))
			if !skip || reason != domain.SkipReasonWeekend {
				t.Fatalf("%s: reason=%q skip=%v; want %q true", testCase.day, reason, skip, domain.SkipReasonWeekend)
			}
		})
	}
}

func TestSkipReason_WeekdayWithNoHolidayPosts(t *testing.T) {
	calendar := newTestCalendar(t, "DE")
	reason, skip := calendar.SkipReason(date(t, "2026-07-07", time.UTC)) // a Tuesday
	if skip {
		t.Fatalf("2026-07-07 skipped with reason %q; want no skip", reason)
	}
}

func TestSkipReason_NoCountryPostsOnHolidays(t *testing.T) {
	calendar := newTestCalendar(t, "")
	// 2026-12-25 is a Friday and a holiday nearly everywhere.
	if reason, skip := calendar.SkipReason(date(t, "2026-12-25", time.UTC)); skip {
		t.Fatalf("2026-12-25 skipped with reason %q despite no configured country", reason)
	}
}

func TestSkipReason_HolidayName(t *testing.T) {
	calendar := newTestCalendar(t, "DE")
	name, ok := calendar.HolidayName(date(t, "2026-12-25", time.UTC))
	if !ok || name != "1. Weihnachtstag" {
		t.Fatalf("HolidayName = (%q, %v); want (%q, true)", name, ok, "1. Weihnachtstag")
	}
}

func TestSkipReason_EasterDerived(t *testing.T) {
	calendar := newTestCalendar(t, "DE")
	cases := map[string]string{
		"2026-04-03": "Karfreitag",
		"2026-04-06": "Ostermontag",
		"2026-05-14": "Christi Himmelfahrt",
		"2026-05-25": "Pfingstmontag",
		"2027-03-26": "Karfreitag",
		"2028-04-17": "Ostermontag",
	}
	for day, want := range cases {
		name, ok := calendar.HolidayName(date(t, day, time.UTC))
		if !ok || name != want {
			t.Errorf("%s: HolidayName = (%q, %v); want (%q, true)", day, name, ok, want)
		}
	}
}

// 2026-07-04 falls on a Saturday, so US federal observance moves Independence
// Day to Friday the 3rd; 2027-07-04 falls on a Sunday and moves to Monday the
// 5th.
func TestSkipReason_USObservedShift(t *testing.T) {
	calendar := newTestCalendar(t, "US")
	cases := map[string]string{
		"2026-07-03": "Independence Day",
		"2027-07-05": "Independence Day",
	}
	for day, want := range cases {
		name, ok := calendar.HolidayName(date(t, day, time.UTC))
		if !ok || name != want {
			t.Errorf("%s: HolidayName = (%q, %v); want (%q, true)", day, name, ok, want)
		}
	}
}

// An observed shift must never overwrite a date another rule already owns.
// Christmas Day 2027 falls on a Saturday and shifts back onto Christmas Eve;
// New Year's Day 2028 falls on a Saturday and shifts back onto New Year's Eve.
// Both days stay holidays, but the rule that owns the date naturally keeps the
// name — otherwise a holiday silently disappears from the table.
func TestSkipReason_USShiftDoesNotClobberOccupiedDate(t *testing.T) {
	calendar := newTestCalendar(t, "US")
	cases := map[string]string{
		"2027-12-24": "Christmas Eve",
		"2027-12-31": "New Year's Eve",
	}
	for day, want := range cases {
		name, ok := calendar.HolidayName(date(t, day, time.UTC))
		if !ok || name != want {
			t.Errorf("%s: HolidayName = (%q, %v); want (%q, true)", day, name, ok, want)
		}
	}
}

// Every rule whose natural date is a weekday must make that day a holiday. Two
// rules may legitimately share a date — Norwegian Whit Monday lands on
// Grunnlovsdag in 2027 — so distinctness is not the invariant; not silently
// losing a working day is. Rules landing naturally on a weekend need no
// assertion: that day is skipped as a weekend either way.
func TestSupportedCountries_EveryWeekdayRuleIsAHoliday(t *testing.T) {
	for _, code := range domain.SupportedCountries() {
		rules, ok := domain.HolidayTable(code)
		if !ok {
			t.Fatalf("%s: no table", code)
		}
		calendar := newTestCalendar(t, string(code))
		for year := 2026; year <= 2035; year++ {
			expanded := calendar.holidaysIn(year)
			for _, rule := range rules {
				natural := ruleDate(rule, year, gregorianEaster(year))
				if isWeekend(natural) {
					continue
				}
				if _, isHoliday := expanded[civil(natural)]; !isHoliday {
					t.Errorf("%s %d: %q resolves to %s (a %s) but that day is not a holiday",
						code, year, rule.Name, natural.Format(time.DateOnly), natural.Weekday())
				}
			}
		}
	}
}

// UK substitute days chain: Christmas 2027 falls on a Saturday and Boxing Day
// on a Sunday, so the substitutes are Monday the 27th and Tuesday the 28th.
func TestSkipReason_UKSubstituteChains(t *testing.T) {
	calendar := newTestCalendar(t, "GB")
	cases := map[string]string{
		"2027-12-27": "Christmas Day",
		"2027-12-28": "Boxing Day",
	}
	for day, want := range cases {
		name, ok := calendar.HolidayName(date(t, day, time.UTC))
		if !ok || name != want {
			t.Errorf("%s: HolidayName = (%q, %v); want (%q, true)", day, name, ok, want)
		}
	}
}

// 2028-01-01 is a Saturday; the UK substitutes Monday the 3rd.
func TestSkipReason_UKSubstituteNewYear(t *testing.T) {
	calendar := newTestCalendar(t, "GB")
	name, ok := calendar.HolidayName(date(t, "2028-01-03", time.UTC))
	if !ok || name != "New Year's Day" {
		t.Fatalf("2028-01-03 = (%q, %v); want (\"New Year's Day\", true)", name, ok)
	}
}

// Continental Europe has no substitute rule: a holiday falling on a weekend is
// simply lost. 2026-10-03 (Tag der Deutschen Einheit) is a Saturday, so the
// following Monday must stay a working day.
func TestSkipReason_NoObservanceLosesWeekendHoliday(t *testing.T) {
	calendar := newTestCalendar(t, "DE")
	if _, ok := calendar.HolidayName(date(t, "2026-10-05", time.UTC)); ok {
		t.Fatal("2026-10-05 treated as a holiday; German holidays do not substitute")
	}
}

func TestSkipReason_NthWeekday(t *testing.T) {
	calendar := newTestCalendar(t, "US")
	cases := map[string]string{
		"2026-01-19": "Martin Luther King Jr. Day", // 3rd Monday of January
		"2026-11-26": "Thanksgiving",               // 4th Thursday of November
		"2026-05-25": "Memorial Day",               // last Monday of May
	}
	for day, want := range cases {
		name, ok := calendar.HolidayName(date(t, day, time.UTC))
		if !ok || name != want {
			t.Errorf("%s: HolidayName = (%q, %v); want (%q, true)", day, name, ok, want)
		}
	}
}

// The last Monday of August 2026 is the 31st — the last day of the month — which
// catches an off-by-one in the "last weekday" walk.
func TestSkipReason_LastWeekdayIsLastDayOfMonth(t *testing.T) {
	calendar := newTestCalendar(t, "GB")
	name, ok := calendar.HolidayName(date(t, "2026-08-31", time.UTC))
	if !ok || name != "Summer bank holiday" {
		t.Fatalf("2026-08-31 = (%q, %v); want (\"Summer bank holiday\", true)", name, ok)
	}
}

// Midsummer Eve is "the Friday on or after June 19". 2026-06-19 is itself a
// Friday, so the anchor date must be returned rather than the following week.
func TestSkipReason_WeekdayOnOrAfterMatchesAnchor(t *testing.T) {
	calendar := newTestCalendar(t, "SE")
	name, ok := calendar.HolidayName(date(t, "2026-06-19", time.UTC))
	if !ok || name != "Midsommarafton" {
		t.Fatalf("2026-06-19 = (%q, %v); want (\"Midsommarafton\", true)", name, ok)
	}
}

// 2027-06-19 is a Saturday, so Midsummer Eve is the following Friday, the 25th.
func TestSkipReason_WeekdayOnOrAfterAdvances(t *testing.T) {
	calendar := newTestCalendar(t, "SE")
	name, ok := calendar.HolidayName(date(t, "2027-06-25", time.UTC))
	if !ok || name != "Midsommarafton" {
		t.Fatalf("2027-06-25 = (%q, %v); want (\"Midsommarafton\", true)", name, ok)
	}
}

// The calendar must read the caller's already-localized time and never re-derive
// a zone: Friday 23:00 UTC is Saturday 01:00 in Berlin and must be skipped as a
// weekend, while Monday 00:30 in Auckland is Sunday 12:30 UTC and must not be.
func TestSkipReason_UsesCallersLocation(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load Europe/Berlin: %v", err)
	}
	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatalf("load Pacific/Auckland: %v", err)
	}
	calendar := newTestCalendar(t, "DE")

	fridayLateUTC := time.Date(2026, time.July, 3, 23, 0, 0, 0, time.UTC) // Friday
	reason, skip := calendar.SkipReason(fridayLateUTC.In(berlin))
	if !skip || reason != domain.SkipReasonWeekend {
		t.Errorf("Friday 23:00 UTC in Berlin: reason=%q skip=%v; want %q true", reason, skip, domain.SkipReasonWeekend)
	}

	sundayMiddayUTC := time.Date(2026, time.July, 5, 12, 30, 0, 0, time.UTC) // Sunday
	if reason, skip := calendar.SkipReason(sundayMiddayUTC.In(auckland)); skip {
		t.Errorf("Sunday 12:30 UTC in Auckland is Monday local: skipped as %q; want no skip", reason)
	}
}

func TestGregorianEaster(t *testing.T) {
	cases := map[int]string{
		1900: "1900-04-15",
		2000: "2000-04-23",
		2026: "2026-04-05",
		2027: "2027-03-28",
		2028: "2028-04-16",
		2029: "2029-04-01",
		2030: "2030-04-21",
		2031: "2031-04-13",
		2100: "2100-03-28",
		2200: "2200-04-06",
	}
	for year, want := range cases {
		got := gregorianEaster(year).Format("2006-01-02")
		if got != want {
			t.Errorf("gregorianEaster(%d) = %s; want %s", year, got, want)
		}
		if weekday := gregorianEaster(year).Weekday(); weekday != time.Sunday {
			t.Errorf("gregorianEaster(%d) is a %s; Easter is always a Sunday", year, weekday)
		}
	}
}

func TestSupportedCountries_AllExpandWithoutPanicOrCollision(t *testing.T) {
	for _, code := range domain.SupportedCountries() {
		calendar := newTestCalendar(t, string(code))
		for year := 2026; year <= 2030; year++ {
			expanded := calendar.holidaysIn(year)
			if len(expanded) == 0 {
				t.Errorf("%s %d: expanded to no holidays", code, year)
			}
			for day := range expanded {
				if day.year != year {
					t.Errorf("%s %d: expansion leaked a %d date (%v)", code, year, day.year, day)
				}
			}
		}
	}
}
