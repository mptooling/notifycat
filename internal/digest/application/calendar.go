package application

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
)

// Calendar answers whether a digest tick falls on a day the team is off. It is
// the application's DigestCalendar.
type Calendar struct {
	country domain.CountryCode
	rules   []domain.HolidayRule
	logger  *slog.Logger

	mu       sync.Mutex
	expanded map[int]map[civilDate]string
}

// civilDate is a location-free calendar date, so a lookup cannot be thrown off
// by a *time.Location mismatch between the clock and the configured zone.
type civilDate struct {
	year  int
	month time.Month
	day   int
}

func civil(t time.Time) civilDate {
	return civilDate{year: t.Year(), month: t.Month(), day: t.Day()}
}

func (d civilDate) time() time.Time {
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC)
}

// NewCalendar resolves the country code to a holiday table.
//
// It never fails. An unset or unrecognized code degrades to weekends-only and
// warns; it does not abort startup the way an invalid cron spec or timezone
// does. Those two decide *when* the server runs at all, whereas the country
// only enriches one feature — so a typo in it must not take the deployment
// down. This mirrors the token degradation elsewhere (an absent GITHUB_TOKEN
// makes path rules inert rather than fatal).
//
// Both warnings fire once, here, rather than on every tick: with no usable
// table there is no way to know that a given day was a holiday, so a per-tick
// line would carry no information and would repeat daily forever.
func NewCalendar(params domain.CalendarParams) *Calendar {
	logger := params.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	calendar := &Calendar{logger: logger, expanded: map[int]map[civilDate]string{}}

	code := domain.CountryCode(strings.ToUpper(strings.TrimSpace(params.Country)))
	if code == "" {
		logger.Warn("digest holidays not configured",
			slog.String("detail", "digest.country is unset; weekends are skipped but public holidays are not"))
		return calendar
	}

	rules, ok := domain.HolidayTable(code)
	if !ok {
		logger.Warn("digest country not recognized",
			slog.String("country", params.Country),
			slog.String("detail", "digest.country is not a supported country code; weekends are skipped but public holidays are not"),
			slog.String("supported", joinCountries(domain.SupportedCountries())))
		return calendar
	}

	calendar.country = code
	calendar.rules = rules
	return calendar
}

// SkipReason reports whether the digest should be suppressed for now's calendar
// day. now is expected to already be in the digest timezone; the calendar reads
// its fields as given and never re-derives a location.
func (c *Calendar) SkipReason(now time.Time) (string, bool) {
	switch now.Weekday() {
	case time.Saturday, time.Sunday:
		return domain.SkipReasonWeekend, true
	}
	if _, ok := c.HolidayName(now); ok {
		return domain.SkipReasonHoliday, true
	}
	return "", false
}

// HolidayName returns the holiday falling on now's calendar day, if any. It
// reports false for every day when no country is configured.
func (c *Calendar) HolidayName(now time.Time) (string, bool) {
	if c.rules == nil {
		return "", false
	}
	name, ok := c.holidaysIn(now.Year())[civil(now)]
	return name, ok
}

// Country is the resolved country code, empty when none is configured.
func (c *Calendar) Country() string {
	return string(c.country)
}

// holidaysIn returns the memoized date-to-name map for a year.
//
// Expansion covers the year before and after as well, then keeps only dates
// landing in year. Observance can move a date across a year boundary — a US New
// Year's Day on a Saturday is observed on the preceding Friday, in December of
// the previous year — so expanding one year in isolation would drop those days.
func (c *Calendar) holidaysIn(year int) map[civilDate]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.expanded[year]; ok {
		return cached
	}
	// The requested year goes first so its own rules own their dates; a
	// neighbour's observed date only fills a day nothing already claims.
	byDate := map[civilDate]string{}
	for _, source := range []int{year, year - 1, year + 1} {
		for day, name := range c.expand(source) {
			if day.year != year {
				continue
			}
			if _, taken := byDate[day]; !taken {
				byDate[day] = name
			}
		}
	}
	c.expanded[year] = byDate
	return byDate
}

// expand resolves every rule to a date in the given year. It runs in two passes
// because ObserveUKSubstitute has to land on a weekday no other holiday already
// claims: the natural dates go down first, then the substitutes resolve against
// them in date order, so consecutive weekend holidays produce consecutive
// substitute days.
func (c *Calendar) expand(year int) map[civilDate]string {
	easterSunday := gregorianEaster(year)
	byDate := map[civilDate]string{}

	type pending struct {
		natural time.Time
		rule    domain.HolidayRule
	}
	var deferred []pending

	for _, rule := range c.rules {
		natural := ruleDate(rule, year, easterSunday)
		if rule.Kind == domain.RuleFixed && isWeekend(natural) && rule.Observance != domain.ObserveNone {
			deferred = append(deferred, pending{natural: natural, rule: rule})
			continue
		}
		if _, taken := byDate[civil(natural)]; !taken {
			byDate[civil(natural)] = rule.Name
		}
	}

	// Resolve substitutes in natural-date order so 25 December claims its
	// weekday before 26 December looks for one.
	for i := 0; i < len(deferred); i++ {
		for j := i + 1; j < len(deferred); j++ {
			if deferred[j].natural.Before(deferred[i].natural) {
				deferred[i], deferred[j] = deferred[j], deferred[i]
			}
		}
	}

	for _, item := range deferred {
		observed, ok := observedDate(item.natural, item.rule.Observance, byDate)
		if !ok {
			continue
		}
		// Never clobber a date that already holds a holiday. A US shift is a
		// fixed move, not a search, so it can land on a day another rule already
		// owns — Christmas Day falling on a Saturday shifts onto Christmas Eve.
		// The day is a holiday either way; the rule that owns the date naturally
		// keeps the name.
		if _, taken := byDate[civil(observed)]; taken {
			continue
		}
		byDate[civil(observed)] = item.rule.Name
	}
	return byDate
}

// observedDate moves a weekend date to the weekday that observes it. It reports
// false when the shift is impossible, which only happens if a substitute search
// runs away.
func observedDate(natural time.Time, observance domain.ObservanceKind, taken map[civilDate]string) (time.Time, bool) {
	switch observance {
	case domain.ObserveUSShift:
		if natural.Weekday() == time.Saturday {
			return natural.AddDate(0, 0, -1), true
		}
		return natural.AddDate(0, 0, 1), true
	case domain.ObserveUKSubstitute:
		// Walk forward to the first weekday nothing else already claims. The
		// bound is generous; a real calendar never needs more than a few days.
		candidate := natural
		for step := 0; step < 14; step++ {
			candidate = candidate.AddDate(0, 0, 1)
			if isWeekend(candidate) {
				continue
			}
			if _, clash := taken[civil(candidate)]; clash {
				continue
			}
			return candidate, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

// ruleDate resolves one rule to its natural date in a year, before observance.
func ruleDate(rule domain.HolidayRule, year int, easterSunday time.Time) time.Time {
	switch rule.Kind {
	case domain.RuleEasterOffset:
		return easterSunday.AddDate(0, 0, rule.EasterDays)
	case domain.RuleNthWeekday:
		return nthWeekdayOf(year, rule.Month, rule.Weekday, rule.Nth)
	case domain.RuleWeekdayOnOrAfter:
		anchor := time.Date(year, rule.Month, rule.Day, 0, 0, 0, 0, time.UTC)
		delta := (int(rule.Weekday) - int(anchor.Weekday()) + 7) % 7
		return anchor.AddDate(0, 0, delta)
	default:
		return time.Date(year, rule.Month, rule.Day, 0, 0, 0, 0, time.UTC)
	}
}

// nthWeekdayOf returns the nth occurrence of a weekday in a month; nth == -1
// means the last one.
func nthWeekdayOf(year int, month time.Month, weekday time.Weekday, nth int) time.Time {
	if nth < 0 {
		// Day 0 of the following month is the last day of this one.
		day := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
		back := (int(day.Weekday()) - int(weekday) + 7) % 7
		return day.AddDate(0, 0, -back)
	}
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	forward := (int(weekday) - int(first.Weekday()) + 7) % 7
	return first.AddDate(0, 0, forward+7*(nth-1))
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// gregorianEaster returns Easter Sunday for a year using the anonymous
// Gregorian ("Meeus/Jones/Butcher") algorithm.
func gregorianEaster(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func joinCountries(codes []domain.CountryCode) string {
	parts := make([]string, len(codes))
	for i, code := range codes {
		parts[i] = string(code)
	}
	return strings.Join(parts, ", ")
}

var _ domain.DigestCalendar = (*Calendar)(nil)
