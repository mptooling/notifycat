package application

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/digest/domain"
)

func calendarLogger(logged *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(logged, nil))
}

func newTestCalendar(t *testing.T, country string) *Calendar {
	t.Helper()

	return NewCalendar(domain.CalendarParams{Country: country, Logger: calendarLogger(&bytes.Buffer{})})
}

func date(t *testing.T, value string, location *time.Location) time.Time {
	t.Helper()

	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	require.NoError(t, err)
	return parsed
}

// assertHolidayNames checks each day resolves to the named holiday.
func assertHolidayNames(t *testing.T, calendar *Calendar, wantByDay map[string]string) {
	t.Helper()

	for day, want := range wantByDay {
		t.Run(day, func(t *testing.T) {
			name, isHoliday := calendar.HolidayName(date(t, day, time.UTC))

			require.True(t, isHoliday, "%s should be a holiday", day)
			assert.Equal(t, want, name)
		})
	}
}

// An unrecognized country must degrade to weekends-only with a warning, never
// abort startup: the digest is one feature and the code is a cosmetic setting,
// so a typo must not take the whole server down.
func TestNewCalendar_WarnsAndIgnoresUnknownCountry(t *testing.T) {
	var logged bytes.Buffer

	calendar := NewCalendar(domain.CalendarParams{Country: "ZZ", Logger: calendarLogger(&logged)})

	assert.Contains(t, logged.String(), "digest country not recognized")
	assert.Contains(t, logged.String(), "ZZ", "the warning names the offending code")
	assert.Contains(t, logged.String(), "DE", "the warning lists the supported codes")

	weekendReason, skipWeekend := calendar.SkipReason(date(t, "2026-07-04", time.UTC))
	assert.True(t, skipWeekend)
	assert.Equal(t, domain.SkipReasonWeekend, weekendReason)

	_, skipHoliday := calendar.SkipReason(date(t, "2026-12-25", time.UTC))
	assert.False(t, skipHoliday, "an unknown country skips no holidays")
}

func TestNewCalendar_UnknownCountryWarnsOnlyOnce(t *testing.T) {
	var logged bytes.Buffer
	calendar := NewCalendar(domain.CalendarParams{Country: "ZZ", Logger: calendarLogger(&logged)})
	require.Equal(t, 1, strings.Count(logged.String(), "digest country not recognized"))
	logged.Reset()

	calendar.SkipReason(date(t, "2026-12-25", time.UTC))

	assert.Empty(t, logged.String(), "a tick must not re-log the warning")
}

func TestNewCalendar_NormalizesCountryCase(t *testing.T) {
	calendar := newTestCalendar(t, "de")

	reason, skip := calendar.SkipReason(date(t, "2026-12-25", time.UTC))

	assert.True(t, skip)
	assert.Equal(t, domain.SkipReasonHoliday, reason)
}

func TestNewCalendar_WarnsOnceWhenCountryUnset(t *testing.T) {
	var logged bytes.Buffer
	calendar := NewCalendar(domain.CalendarParams{Logger: calendarLogger(&logged)})
	require.Equal(t, 1, strings.Count(logged.String(), "digest holidays not configured"))
	logged.Reset()

	calendar.SkipReason(date(t, "2026-12-25", time.UTC))
	calendar.SkipReason(date(t, "2026-12-26", time.UTC))

	assert.Empty(t, logged.String())
}

func TestNewCalendar_DoesNotWarnWhenCountrySet(t *testing.T) {
	var logged bytes.Buffer

	NewCalendar(domain.CalendarParams{Country: "DE", Logger: calendarLogger(&logged)})

	assert.NotContains(t, logged.String(), "digest holidays not configured")
}

func TestSkipReason_Weekend(t *testing.T) {
	testCases := []struct {
		name    string
		country string
		day     string
	}{
		{"saturday with country", "DE", "2026-07-04"},
		{"sunday with country", "DE", "2026-07-05"},
		{"saturday without country", "", "2026-07-04"},
		{"sunday without country", "", "2026-07-05"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calendar := newTestCalendar(t, testCase.country)

			reason, skip := calendar.SkipReason(date(t, testCase.day, time.UTC))

			assert.True(t, skip)
			assert.Equal(t, domain.SkipReasonWeekend, reason)
		})
	}
}

func TestSkipReason_WeekdayWithNoHolidayPosts(t *testing.T) {
	calendar := newTestCalendar(t, "DE")

	// 2026-07-07 is a Tuesday.
	_, skip := calendar.SkipReason(date(t, "2026-07-07", time.UTC))

	assert.False(t, skip)
}

func TestSkipReason_NoCountryPostsOnHolidays(t *testing.T) {
	calendar := newTestCalendar(t, "")

	// 2026-12-25 is a Friday and a holiday nearly everywhere.
	_, skip := calendar.SkipReason(date(t, "2026-12-25", time.UTC))

	assert.False(t, skip)
}

func TestSkipReason_HolidayName(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "DE"), map[string]string{
		"2026-12-25": "1. Weihnachtstag",
	})
}

func TestSkipReason_EasterDerived(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "DE"), map[string]string{
		"2026-04-03": "Karfreitag",
		"2026-04-06": "Ostermontag",
		"2026-05-14": "Christi Himmelfahrt",
		"2026-05-25": "Pfingstmontag",
		"2027-03-26": "Karfreitag",
		"2028-04-17": "Ostermontag",
	})
}

// 2026-07-04 falls on a Saturday, so US federal observance moves Independence
// Day to Friday the 3rd; 2027-07-04 falls on a Sunday and moves to Monday the
// 5th.
func TestSkipReason_USObservedShift(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "US"), map[string]string{
		"2026-07-03": "Independence Day",
		"2027-07-05": "Independence Day",
	})
}

// An observed shift must never overwrite a date another rule already owns.
// Christmas Day 2027 falls on a Saturday and shifts back onto Christmas Eve;
// New Year's Day 2028 falls on a Saturday and shifts back onto New Year's Eve.
// Both days stay holidays, but the rule that owns the date naturally keeps the
// name — otherwise a holiday silently disappears from the table.
func TestSkipReason_USShiftDoesNotClobberOccupiedDate(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "US"), map[string]string{
		"2027-12-24": "Christmas Eve",
		"2027-12-31": "New Year's Eve",
	})
}

// Every rule whose natural date is a weekday must make that day a holiday. Two
// rules may legitimately share a date — Norwegian Whit Monday lands on
// Grunnlovsdag in 2027 — so distinctness is not the invariant; not silently
// losing a working day is. Rules landing naturally on a weekend need no
// assertion: that day is skipped as a weekend either way.
func TestSupportedCountries_EveryWeekdayRuleIsAHoliday(t *testing.T) {
	for _, code := range domain.SupportedCountries() {
		t.Run(string(code), func(t *testing.T) {
			rules, ok := domain.HolidayTable(code)
			require.True(t, ok, "no table for %s", code)
			calendar := newTestCalendar(t, string(code))

			for year := 2026; year <= 2035; year++ {
				expanded := calendar.holidaysIn(year)
				for _, rule := range rules {
					natural := ruleDate(rule, year, gregorianEaster(year))
					if isWeekend(natural) {
						continue
					}
					assert.Contains(t, expanded, civil(natural),
						"%d: %q resolves to %s (a %s) but that day is not a holiday",
						year, rule.Name, natural.Format(time.DateOnly), natural.Weekday())
				}
			}
		})
	}
}

// UK substitute days chain: Christmas 2027 falls on a Saturday and Boxing Day
// on a Sunday, so the substitutes are Monday the 27th and Tuesday the 28th.
func TestSkipReason_UKSubstituteChains(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "GB"), map[string]string{
		"2027-12-27": "Christmas Day",
		"2027-12-28": "Boxing Day",
	})
}

// 2028-01-01 is a Saturday; the UK substitutes Monday the 3rd.
func TestSkipReason_UKSubstituteNewYear(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "GB"), map[string]string{
		"2028-01-03": "New Year's Day",
	})
}

// Continental Europe has no substitute rule: a holiday falling on a weekend is
// simply lost. 2026-10-03 (Tag der Deutschen Einheit) is a Saturday, so the
// following Monday must stay a working day.
func TestSkipReason_NoObservanceLosesWeekendHoliday(t *testing.T) {
	calendar := newTestCalendar(t, "DE")

	_, isHoliday := calendar.HolidayName(date(t, "2026-10-05", time.UTC))

	assert.False(t, isHoliday, "German holidays do not substitute")
}

func TestSkipReason_NthWeekday(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "US"), map[string]string{
		"2026-01-19": "Martin Luther King Jr. Day",
		"2026-11-26": "Thanksgiving",
		"2026-05-25": "Memorial Day",
	})
}

// The last Monday of August 2026 is the 31st — the last day of the month — which
// catches an off-by-one in the "last weekday" walk.
func TestSkipReason_LastWeekdayIsLastDayOfMonth(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "GB"), map[string]string{
		"2026-08-31": "Summer bank holiday",
	})
}

// Midsummer Eve is "the Friday on or after June 19". 2026-06-19 is itself a
// Friday, so the anchor date must be returned rather than the following week;
// 2027-06-19 is a Saturday, so it advances to the 25th.
func TestSkipReason_WeekdayOnOrAfter(t *testing.T) {
	assertHolidayNames(t, newTestCalendar(t, "SE"), map[string]string{
		"2026-06-19": "Midsommarafton",
		"2027-06-25": "Midsommarafton",
	})
}

// The calendar must read the caller's already-localized time and never re-derive
// a zone: Friday 23:00 UTC is Saturday 01:00 in Berlin and must be skipped as a
// weekend, while Monday 00:30 in Auckland is Sunday 12:30 UTC and must not be.
func TestSkipReason_UsesCallersLocation(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	auckland, err := time.LoadLocation("Pacific/Auckland")
	require.NoError(t, err)
	calendar := newTestCalendar(t, "DE")
	fridayLateUTC := time.Date(2026, time.July, 3, 23, 0, 0, 0, time.UTC)
	sundayMiddayUTC := time.Date(2026, time.July, 5, 12, 30, 0, 0, time.UTC)

	berlinReason, skipBerlin := calendar.SkipReason(fridayLateUTC.In(berlin))
	_, skipAuckland := calendar.SkipReason(sundayMiddayUTC.In(auckland))

	assert.True(t, skipBerlin, "Friday 23:00 UTC is already Saturday in Berlin")
	assert.Equal(t, domain.SkipReasonWeekend, berlinReason)
	assert.False(t, skipAuckland, "Sunday 12:30 UTC is already Monday in Auckland")
}

func TestGregorianEaster(t *testing.T) {
	wantByYear := map[int]string{
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

	for year, want := range wantByYear {
		t.Run(want, func(t *testing.T) {
			easter := gregorianEaster(year)

			assert.Equal(t, want, easter.Format(time.DateOnly))
			assert.Equal(t, time.Sunday, easter.Weekday(), "Easter is always a Sunday")
		})
	}
}

func TestSupportedCountries_AllExpandWithoutPanicOrCollision(t *testing.T) {
	for _, code := range domain.SupportedCountries() {
		t.Run(string(code), func(t *testing.T) {
			calendar := newTestCalendar(t, string(code))

			for year := 2026; year <= 2030; year++ {
				expanded := calendar.holidaysIn(year)

				assert.NotEmpty(t, expanded, "%d expanded to no holidays", year)
				for day := range expanded {
					assert.Equal(t, year, day.year, "%d: expansion leaked a %d date (%v)", year, day.year, day)
				}
			}
		})
	}
}
