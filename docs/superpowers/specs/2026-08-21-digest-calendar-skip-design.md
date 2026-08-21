# Digest calendar skip — design

Issue: [#170 — feat(digest): skip weekends and common holidays](https://github.com/mptooling/notifycat/issues/170)
Date: 2026-08-21
Status: implemented

## Problem

The stuck-PR digest fires on every cron tick with no awareness of the calendar. The default schedule (`DefaultDigestSchedule = "0 9 * * *"`, `internal/routing/domain/constants.go`) posts every day — weekends and public holidays included. Nobody acts on a Saturday digest; the same PRs get re-announced Monday. Cron day-of-week can express "weekdays only" but every operator has to know to write it, and cron cannot express holidays at all.

## Decisions taken

Recorded so the reasoning survives the PR. Each was an explicit choice, not a default.

| Decision | Choice | Rationale |
| --- | --- | --- |
| Weekend skip | **Always on, not configurable** | Deliberate breaking change. A Saturday digest is never wanted; making it a toggle invites a wrong setting. |
| Holiday data | **Hand-written rule tables, no dependency** | `go.mod` has 7 direct deps, all load-bearing. `github.com/rickar/cal/v2` was evaluated (v2.1.29, BSD, zero transitive deps, covers all 19 countries) and rejected in favour of owning the tables. |
| Holiday scope | **Country only, no subdivisions** | `digest.country: DE` resolves to the federal set. Region support (`DE-BY` → Bayern) is additive later with no schema break. |
| Dec 24 / Dec 31 | **In every table, including US** | Not legal public holidays in most of these countries, but de-facto shutdown days. Satisfies the issue's Dec 31 acceptance criterion. |
| Country absent | **Weekends skipped, holidays posted, warning logged once at startup** | One behavior delta on upgrade instead of two. No silent default country. |
| Config scope | **Global only** | Mirrors `digest.timezone`, which is already rejected on a repo tier. One deployment, one team calendar. |
| Ukraine | **Fixed dates only** | Orthodox Pascha needs a second Julian-based algorithm; both Pascha and Trinity fall on a Sunday, already skipped as a weekend, so only Easter Monday would ever have mattered. Documented as a known gap. |

### Known risk, accepted

19 hand-written calendars is roughly 230 rules the project owns and verifies by hand, with no upstream to inherit fixes from. This is the exact problem `rickar/cal` exists to solve. The trade was made knowingly to keep `go.mod` at 7 direct dependencies. Mitigation: every rule is asserted against real dates for 2026–2030 in table-driven tests, and each country's table carries a source comment.

## Architecture

Three layers of the existing `digest` domain, no new domain.

```
internal/digest/
  domain/
    enums.go        RuleKind, ObservanceKind, CountryCode  (new values)
    holidays.go     HolidayRule + the 19 country tables    (new file)
    interfaces.go   DigestCalendar port                    (new interface)
    models.go       CalendarParams, SchedulerParams.Calendar
  application/
    calendar.go     Calendar — rule expansion, Easter math (new file)
    scheduler.go    tick consults the calendar before the job
```

Rule tables are constant-like data, so they live in the domain layer per the architecture rules. The evaluator is a use case, so it lives in the application layer behind a domain interface. Nothing in either layer touches an SDK, so no infrastructure adapter is needed.

### Rule representation

```go
// domain/holidays.go
type HolidayRule struct {
	Name       string
	Kind       RuleKind
	Month      time.Month     // RuleFixed, RuleNthWeekday, RuleWeekdayOnOrAfter
	Day        int            // RuleFixed, RuleWeekdayOnOrAfter (the "on or after" day)
	EasterDays int            // RuleEasterOffset: days from Easter Sunday
	Weekday    time.Weekday   // RuleNthWeekday, RuleWeekdayOnOrAfter
	Nth        int            // RuleNthWeekday: 1..5, or -1 for last
	Observance ObservanceKind
}
```

```go
// domain/enums.go
type RuleKind uint8
const (
	RuleFixed            RuleKind = iota // Month + Day
	RuleEasterOffset                     // EasterDays from Easter Sunday
	RuleNthWeekday                       // Nth Weekday of Month; -1 = last
	RuleWeekdayOnOrAfter                 // first Weekday on or after Month/Day
)

type ObservanceKind uint8
const (
	ObserveNone          ObservanceKind = iota // weekend collision: the day is lost
	ObserveUSShift                             // Sat -> preceding Fri, Sun -> following Mon
	ObserveUKSubstitute                        // -> next weekday not already a holiday
)
```

`RuleWeekdayOnOrAfter` exists for the Nordics: Swedish `Midsommarafton` and Finnish `Juhannusaatto` are "the Friday on or after June 19", which neither a fixed date nor an nth-weekday rule can express.

Rejected alternatives: a closure per holiday (`func(year int) time.Time`) — flexible, but the tables stop being reviewable data and cannot be asserted as constants; a precomputed date set for N years — trivial evaluator, but it has a horizon that silently expires.

### Easter

Anonymous Gregorian algorithm (Meeus/Jones/Butcher), verified against known dates before the tables were written:

| Year | Easter | Good Friday | Easter Mon | Ascension | Whit Mon | Corpus Christi |
| --- | --- | --- | --- | --- | --- | --- |
| 2026 | Apr 5 (Sun) | Apr 3 (Fri) | Apr 6 (Mon) | May 14 (Thu) | May 25 (Mon) | Jun 4 (Thu) |
| 2027 | Mar 28 (Sun) | Mar 26 (Fri) | Mar 29 (Mon) | May 6 (Thu) | May 17 (Mon) | May 27 (Thu) |
| 2028 | Apr 16 (Sun) | Apr 14 (Fri) | Apr 17 (Mon) | May 25 (Thu) | Jun 5 (Mon) | Jun 15 (Thu) |
| 2029 | Apr 1 (Sun) | Mar 30 (Fri) | Apr 2 (Mon) | May 10 (Thu) | May 21 (Mon) | May 31 (Thu) |
| 2030 | Apr 21 (Sun) | Apr 19 (Fri) | Apr 22 (Mon) | May 30 (Thu) | Jun 10 (Mon) | Jun 20 (Thu) |

Consequence worth stating: **every Easter-derived holiday falls on a fixed weekday**, so an `RuleEasterOffset` rule can never collide with a weekend and never needs observance handling. Only `RuleFixed` rules do.

### Port and evaluation

```go
// domain/interfaces.go

// DigestCalendar decides whether a digest tick on a given day should be
// suppressed. It is consulted by the scheduler before the job runs, so a
// skipped day makes no Slack call at all. The returned reason is the value of
// the `reason` field on the "skipped digest" log line.
type DigestCalendar interface {
	SkipReason(now time.Time) (reason string, skip bool)
}
```

`application.Calendar` implements it:

1. **Weekend first.** `Saturday`/`Sunday` → `(SkipReasonWeekend, true)`. The holiday table is never consulted, so a deployment with no country still gets the weekend skip.
2. **Holiday.** Expand the country's rules for `now.Year()` into a `map[civilDate]string` (date → holiday name), memoized per year in the `Calendar`. On a hit → `(SkipReasonHoliday, true)`.
3. Otherwise `("", false)`.

`civilDate` is a `struct{ year int; month time.Month; day int }` — a comparable map key that carries no location, so the lookup cannot be confused by a `*time.Location` mismatch. `now` is already `In(tz)` before it reaches the calendar.

Observance is applied while expanding, not at lookup time:
- `ObserveUSShift` — a Saturday date moves to the preceding Friday, a Sunday date to the following Monday.
- `ObserveUKSubstitute` — the date moves forward to the first weekday not already claimed by another holiday in the same year. This is what makes 2027 correct: Christmas falls Saturday and Boxing Day Sunday, so the substitutes are Monday Dec 27 **and** Tuesday Dec 28.
- `ObserveNone` — a weekend collision means the holiday is simply lost, which is the real rule in Germany, France, and most of continental Europe.

Because substitution can depend on other rules in the same year, expansion runs in two passes: all non-observed dates first, then the observed ones resolve against that set.

Constructor validates the country code and returns an error for an unknown one, so an fx `Provide` failure aborts the boot — same fail-fast contract as an invalid cron spec or IANA zone.

### Wiring

`internal/digest/module.go`:
- `Config` grows `Country string`.
- New `provideCalendar(cfg Config, logger *slog.Logger) (domain.DigestCalendar, error)`, bound via `fx.Annotate`.
- `provideSchedulerParams` passes the calendar through; `domain.SchedulerParams` grows `Calendar domain.DigestCalendar`.

`internal/digest/application/scheduler.go` — the tick closure gains the guard:

```go
if reason, skip := s.calendar.SkipReason(time.Now().In(s.tz)); skip {
	s.logger.Info("skipped digest", slog.String("schedule", spec), slog.String("reason", reason), ...)
	return
}
```

The check lives in the scheduler because the scheduler already owns `tz` and is the authority on *when* the digest runs; skipping there means zero Slack calls and one log line per tick. A `ScheduleJob` decorator wrapping the reporter was considered — cleaner separation of concerns, but it needs `fx.Decorate` for a second binding of the same port, which is not worth the indirection for one call site.

`Reporter` is untouched. `Reporter.Report(ctx)` (the all-repos variant) has no production caller — only `ReportSchedule` runs in the server — so a manual/all-repos pass, if one is ever added, is deliberately not calendar-gated.

The scheduler needs a clock it can control in tests. It currently calls nothing time-related directly; `SchedulerParams` gains `Now func() time.Time` defaulting to `time.Now`, matching the existing `ReporterParams.Now` convention.

### Config

New key, global only:

```yaml
digest:
  enabled: true
  schedule: "0 9 * * 1-5"
  timezone: "Europe/Berlin"
  country: DE              # new; empty/absent = weekends only
```

- `internal/routing/domain/models.go` — `DigestConfig` grows `Country string`, documented as global-only alongside `Timezone`.
- `internal/routing/infrastructure/config_decode.go` — `digestConfigWire` grows a `Country` field and a `case "country":` in `UnmarshalYAML` (the decoder rejects unknown keys, so the case is mandatory); `toDomain` carries it across; the repo-tier guard next to the existing `d.Timezone != ""` check rejects `country` on a tier with a message pointing at the global section.
- `internal/platform/config/config.go` — no change needed; `cfg.Digest` already carries the whole struct through.
- `internal/routing/application/provider.go` — `Digest()` copies `Country` from the global section the same way it copies `Timezone` (line 36); `DigestFor`'s per-tier `apply` does **not** touch it.
- `internal/runtime` — passes `cfg.Digest.Country` into `digest.Config`.

Country codes are ISO 3166-1 alpha-2, case-insensitive on input (`de` and `DE` both work), stored normalized.

### Logging

Two skip reasons, matching the `ignored webhook event` reason contract:

```
INFO  skipped digest  schedule="0 9 * * *"  reason=weekend  date=2026-07-04  weekday=Saturday
INFO  skipped digest  schedule="0 9 * * *"  reason=holiday  date=2026-12-25  country=DE  holiday="1. Weihnachtstag"
```

Reason constants live in `domain/enums.go` (`SkipReasonWeekend = "weekend"`, `SkipReasonHoliday = "holiday"`).

One startup warning when the digest is enabled and no country is set:

```
WARN  digest holidays not configured  detail="digest.country is unset; weekends are skipped but public holidays are not"
```

Emitted once from the `Calendar` constructor, not per tick: with no country there is no way to know that a given day *was* a holiday, so a per-tick line would carry no information and would fire daily forever.

## Country tables

19 countries. Statutory public holidays, plus Dec 24 and Dec 31 in every table per the decision above. Rules that always land on a weekend (Easter Sunday, Pentecost Sunday, Swedish/Finnish All Saints' Saturday) are omitted — the weekend check already covers them and listing them is dead data.

Legend: `F` fixed, `E±n` Easter offset, `N` nth weekday, `W` weekday-on-or-after. Observance column: `–` none, `US` US shift, `UK` substitute.

### US — 13 rules, observance `US` on all fixed

| Rule | Name |
| --- | --- |
| F 01-01 | New Year's Day |
| N 3rd Mon Jan | Martin Luther King Jr. Day |
| N 3rd Mon Feb | Washington's Birthday |
| N last Mon May | Memorial Day |
| F 06-19 | Juneteenth |
| F 07-04 | Independence Day |
| N 1st Mon Sep | Labor Day |
| N 2nd Mon Oct | Columbus Day |
| F 11-11 | Veterans Day |
| N 4th Thu Nov | Thanksgiving |
| F 12-24 | Christmas Eve *(de-facto)* |
| F 12-25 | Christmas Day |
| F 12-31 | New Year's Eve *(de-facto)* |

### GB — 10 rules, observance `UK` on all fixed

England and Wales. Scotland (Aug 2 substitute, Nov 30) and Northern Ireland (Mar 17, Jul 12) differ — out of scope with subdivisions.

| Rule | Name |
| --- | --- |
| F 01-01 | New Year's Day |
| E−2 | Good Friday |
| E+1 | Easter Monday |
| N 1st Mon May | Early May bank holiday |
| N last Mon May | Spring bank holiday |
| N last Mon Aug | Summer bank holiday |
| F 12-24 | Christmas Eve *(de-facto)* |
| F 12-25 | Christmas Day |
| F 12-26 | Boxing Day |
| F 12-31 | New Year's Eve *(de-facto)* |

### IE — 12 rules, observance `UK` on all fixed

| Rule | Name |
| --- | --- |
| F 01-01 | New Year's Day |
| N 1st Mon Feb | St Brigid's Day 🟡 |
| F 03-17 | St Patrick's Day |
| E+1 | Easter Monday |
| N 1st Mon May | May Day |
| N 1st Mon Jun | June holiday |
| N 1st Mon Aug | August holiday |
| N last Mon Oct | October holiday |
| F 12-24 | Christmas Eve *(de-facto)* |
| F 12-25 | Christmas Day |
| F 12-26 | St Stephen's Day |
| F 12-31 | New Year's Eve *(de-facto)* |

🟡 The statutory rule is "Feb 1 when it falls on a Friday, otherwise the first Monday in February". Modelled as the first Monday, which is wrong in years where Feb 1 is a Friday (2030, 2036). Good Friday is omitted — it is a bank holiday in Ireland but not a statutory public holiday.

### DE — 11 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Neujahr |
| E−2 | Karfreitag |
| E+1 | Ostermontag |
| F 05-01 | Tag der Arbeit |
| E+39 | Christi Himmelfahrt |
| E+50 | Pfingstmontag |
| F 10-03 | Tag der Deutschen Einheit |
| F 12-24 | Heiligabend *(de-facto)* |
| F 12-25 | 1. Weihnachtstag |
| F 12-26 | 2. Weihnachtstag |
| F 12-31 | Silvester *(de-facto)* |

Bundesland-only days (Heilige Drei Könige, Fronleichnam, Mariä Himmelfahrt, Reformationstag, Allerheiligen, Buß- und Bettag) are excluded — they need subdivision support.

### AT — 15 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Neujahr |
| F 01-06 | Heilige Drei Könige |
| E+1 | Ostermontag |
| F 05-01 | Staatsfeiertag |
| E+39 | Christi Himmelfahrt |
| E+50 | Pfingstmontag |
| E+60 | Fronleichnam |
| F 08-15 | Mariä Himmelfahrt |
| F 10-26 | Nationalfeiertag |
| F 11-01 | Allerheiligen |
| F 12-08 | Mariä Empfängnis |
| F 12-24 | Heiligabend *(de-facto)* |
| F 12-25 | Christtag |
| F 12-26 | Stefanitag |
| F 12-31 | Silvester *(de-facto)* |

Good Friday is not an Austrian public holiday (removed 2019).

### CH — 10 rules, observance `–` 🟡

| Rule | Name |
| --- | --- |
| F 01-01 | Neujahr |
| E−2 | Karfreitag |
| E+1 | Ostermontag |
| E+39 | Auffahrt |
| E+50 | Pfingstmontag |
| F 08-01 | Bundesfeier |
| F 12-24 | Heiligabend *(de-facto)* |
| F 12-25 | Weihnachten |
| F 12-26 | Stephanstag |
| F 12-31 | Silvester *(de-facto)* |

🟡 Only Aug 1 is a federal holiday; the rest are cantonal. This is the set observed in most German-speaking cantons. Geneva, Ticino, and Valais differ materially.

### NL — 10 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Nieuwjaarsdag |
| E−2 | Goede Vrijdag 🟡 |
| E+1 | Tweede Paasdag |
| F 04-27 | Koningsdag |
| E+39 | Hemelvaartsdag |
| E+50 | Tweede Pinksterdag |
| F 12-24 | Kerstavond *(de-facto)* |
| F 12-25 | Eerste Kerstdag |
| F 12-26 | Tweede Kerstdag |
| F 12-31 | Oudejaarsdag *(de-facto)* |

🟡 Good Friday is a national holiday in the Netherlands but not a mandatory day off; many employers work it. Bevrijdingsdag (May 5) is a general day off only every fifth year and is omitted.

### BE — 12 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Nieuwjaar |
| E+1 | Paasmaandag |
| F 05-01 | Dag van de Arbeid |
| E+39 | Onze-Lieve-Heer-Hemelvaart |
| E+50 | Pinkstermaandag |
| F 07-21 | Nationale feestdag |
| F 08-15 | Onze-Lieve-Vrouw-Hemelvaart |
| F 11-01 | Allerheiligen |
| F 11-11 | Wapenstilstand |
| F 12-24 | Kerstavond *(de-facto)* |
| F 12-25 | Kerstmis |
| F 12-31 | Oudejaar *(de-facto)* |

### LU — 13 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Neijoerschdag |
| E+1 | Ouschterméindag |
| F 05-01 | Dag vun der Aarbecht |
| F 05-09 | Europadag |
| E+39 | Christi Himmelfaart |
| E+50 | Péngschtméindag |
| F 06-23 | Nationalfeierdag |
| F 08-15 | Mariä Himmelfaart |
| F 11-01 | Allerhellgen |
| F 12-24 | Hellegabend *(de-facto)* |
| F 12-25 | Chrëschtdag |
| F 12-26 | Stefansdag |
| F 12-31 | Silvester *(de-facto)* |

### FR — 13 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Jour de l'An |
| E+1 | Lundi de Pâques |
| F 05-01 | Fête du Travail |
| F 05-08 | Victoire 1945 |
| E+39 | Ascension |
| E+50 | Lundi de Pentecôte |
| F 07-14 | Fête nationale |
| F 08-15 | Assomption |
| F 11-01 | Toussaint |
| F 11-11 | Armistice |
| F 12-24 | Réveillon de Noël *(de-facto)* |
| F 12-25 | Noël |
| F 12-31 | Saint-Sylvestre *(de-facto)* |

Alsace-Moselle additionally observes Good Friday and Dec 26 — subdivision scope.

### ES — 12 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Año Nuevo |
| F 01-06 | Epifanía del Señor |
| E−2 | Viernes Santo |
| F 05-01 | Día del Trabajador |
| F 08-15 | Asunción de la Virgen |
| F 10-12 | Fiesta Nacional |
| F 11-01 | Todos los Santos |
| F 12-06 | Día de la Constitución |
| F 12-08 | Inmaculada Concepción |
| F 12-24 | Nochebuena *(de-facto)* |
| F 12-25 | Navidad |
| F 12-31 | Nochevieja *(de-facto)* |

Regional days (Maundy Thursday in most autonomous communities, Jan 6 variations, regional fiestas) are subdivision scope.

### PT — 14 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Ano Novo |
| E−2 | Sexta-feira Santa |
| F 04-25 | Dia da Liberdade |
| F 05-01 | Dia do Trabalhador |
| E+60 | Corpo de Deus |
| F 06-10 | Dia de Portugal |
| F 08-15 | Assunção de Nossa Senhora |
| F 10-05 | Implantação da República |
| F 11-01 | Todos os Santos |
| F 12-01 | Restauração da Independência |
| F 12-08 | Imaculada Conceição |
| F 12-24 | Véspera de Natal *(de-facto)* |
| F 12-25 | Natal |
| F 12-31 | Véspera de Ano Novo *(de-facto)* |

Carnival (Easter −47) is not statutory nationally and is omitted.

### IT — 13 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Capodanno |
| F 01-06 | Epifania |
| E+1 | Lunedì dell'Angelo |
| F 04-25 | Festa della Liberazione |
| F 05-01 | Festa del Lavoro |
| F 06-02 | Festa della Repubblica |
| F 08-15 | Ferragosto |
| F 11-01 | Ognissanti |
| F 12-08 | Immacolata Concezione |
| F 12-24 | Vigilia di Natale *(de-facto)* |
| F 12-25 | Natale |
| F 12-26 | Santo Stefano |
| F 12-31 | San Silvestro *(de-facto)* |

### SE — 12 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Nyårsdagen |
| F 01-06 | Trettondedag jul |
| E−2 | Långfredagen |
| E+1 | Annandag påsk |
| F 05-01 | Första maj |
| E+39 | Kristi himmelsfärdsdag |
| F 06-06 | Sveriges nationaldag |
| W Fri on/after 06-19 | Midsommarafton *(de-facto)* |
| F 12-24 | Julafton *(de-facto)* |
| F 12-25 | Juldagen |
| F 12-26 | Annandag jul |
| F 12-31 | Nyårsafton *(de-facto)* |

Midsommardagen and Alla helgons dag always fall on a Saturday — omitted, already covered by the weekend check. Julafton and Nyårsafton are not statutory in Sweden but are near-universal days off.

### NO — 12 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Første nyttårsdag |
| E−3 | Skjærtorsdag |
| E−2 | Langfredag |
| E+1 | Andre påskedag |
| F 05-01 | Arbeidernes dag |
| F 05-17 | Grunnlovsdag |
| E+39 | Kristi himmelfartsdag |
| E+50 | Andre pinsedag |
| F 12-24 | Julaften *(de-facto)* |
| F 12-25 | Første juledag |
| F 12-26 | Andre juledag |
| F 12-31 | Nyttårsaften *(de-facto)* |

### DK — 10 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Nytårsdag |
| E−3 | Skærtorsdag |
| E−2 | Langfredag |
| E+1 | 2. påskedag |
| E+39 | Kristi himmelfartsdag |
| E+50 | 2. pinsedag |
| F 12-24 | Juleaftensdag *(de-facto)* |
| F 12-25 | 1. juledag |
| F 12-26 | 2. juledag |
| F 12-31 | Nytårsaftensdag *(de-facto)* |

Store bededag was abolished as a public holiday from 2024 and is deliberately absent. Grundlovsdag (Jun 5) is not statutory and is omitted, even though many employers grant it.

### FI — 12 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Uudenvuodenpäivä |
| F 01-06 | Loppiainen |
| E−2 | Pitkäperjantai |
| E+1 | 2. pääsiäispäivä |
| F 05-01 | Vappu |
| E+39 | Helatorstai |
| W Fri on/after 06-19 | Juhannusaatto *(de-facto)* |
| F 12-06 | Itsenäisyyspäivä |
| F 12-24 | Jouluaatto *(de-facto)* |
| F 12-25 | Joulupäivä |
| F 12-26 | Tapaninpäivä |
| F 12-31 | Uudenvuodenaatto *(de-facto)* |

Juhannuspäivä and Pyhäinpäivä always fall on a Saturday — omitted.

### PL — 13 rules, observance `–`

| Rule | Name |
| --- | --- |
| F 01-01 | Nowy Rok |
| F 01-06 | Święto Trzech Króli |
| E+1 | Poniedziałek Wielkanocny |
| F 05-01 | Święto Pracy |
| F 05-03 | Święto Konstytucji 3 Maja |
| E+60 | Boże Ciało |
| F 08-15 | Wniebowzięcie NMP |
| F 11-01 | Wszystkich Świętych |
| F 11-11 | Święto Niepodległości |
| F 12-24 | Wigilia Bożego Narodzenia |
| F 12-25 | Boże Narodzenie |
| F 12-26 | Drugi dzień Bożego Narodzenia |
| F 12-31 | Sylwester *(de-facto)* |

Dec 24 became a statutory public holiday in Poland in 2025 — it is a real holiday here, not a de-facto one.

### UA — 9 rules, observance `–` 🟡

| Rule | Name |
| --- | --- |
| F 01-01 | Новий рік |
| F 03-08 | Міжнародний жіночий день |
| F 05-01 | День праці |
| F 06-28 | День Конституції |
| F 08-24 | День Незалежності |
| F 10-01 | День захисників і захисниць |
| F 12-24 | Святвечір *(de-facto)* |
| F 12-25 | Різдво |
| F 12-31 | Новорічний вечір *(de-facto)* |

🟡 Two known gaps, both accepted: Pascha and Trinity Sunday are omitted because they need Julian Easter arithmetic (and both fall on a Sunday regardless); and martial law currently suspends public holidays in Ukraine entirely, which no calendar can model.

## Testing

TDD throughout — each country table lands with its assertions in the same commit.

**Per-country date tables.** For each of the 19 countries, assert the expanded date set for 2026–2030 against hand-checked expected dates. 2027 is the load-bearing year: Christmas falls Saturday and Boxing Day Sunday, which exercises `ObserveUKSubstitute` producing two shifted days (Mon Dec 27, Tue Dec 28) and `ObserveUSShift` producing Fri Dec 24. 2028 exercises New Year's Day on a Saturday.

**Easter.** Assert the algorithm against the six years in the table above, plus a century-boundary case (2100: Mar 28) to catch a botched `b/4` term.

**Rule kinds.** `RuleNthWeekday` with `Nth: -1` on a month whose last weekday is the 29th vs the 31st; `RuleWeekdayOnOrAfter` when the anchor date *is* the target weekday (must return the anchor, not the following week) — Jun 19 2026 is a Friday, which covers it.

**Timezone boundaries.** A `now` of Friday 23:00 UTC in `Europe/Berlin` is Saturday 01:00 local → skipped as weekend. A `now` of Monday 00:30 in `Pacific/Auckland` is Sunday 12:30 UTC → *not* skipped. Both assert that the calendar reads the caller's already-localized time and never re-derives a zone.

**No-country path.** Weekend skipped, Dec 25 not skipped, warning logged exactly once at construction and not on any tick.

**Scheduler.** A skipped tick calls the job zero times and logs `skipped digest` with the right reason; a normal tick calls it once and logs nothing extra.

**Config.** `country: US` on a repo tier fails decoding with a message naming the global section; an unknown country code fails the `Calendar` constructor; `country: de` normalizes to `DE`.

## Documentation

| File | Change |
| --- | --- |
| `docs/digest.md` | New "Weekends and holidays" section: weekends always skipped and why it is not configurable, `digest.country` with the supported-code table, the no-country warning, and the 🟡 caveats (Swiss cantons, Irish St Brigid's Day, Ukraine). |
| `docs/configuration.md` | New `digest.country` row in the `### digest` table (line ~118), worded like the existing global-only `digest.timezone` row. |
| `docs/troubleshooting.md` | "The digest never posts" gains weekend/holiday as the first thing to check, with the `skipped digest` log line to grep for; the startup-error table gains a row for an unrecognized country code. |
| `docs/upgrading.md` | New section for the release: weekends are now skipped unconditionally, a deliberate breaking change; an operator whose schedule deliberately targets a weekend will no longer get a digest. |

Release-please handles `CHANGELOG.md` from the PR title. Pre-1.0, a breaking change bumps the minor version: 0.22 → 0.23. The PR body must avoid the literal string that release-please parses as a breaking-change footer.

## Found during implementation

Three things the design did not anticipate. All are fixed in the code and covered by tests.

**Observance can clobber another rule's date.** Christmas Day 2027 falls on a Saturday, so the US shift moves it back to Friday 24 December — a day the de-facto Christmas Eve rule already owns. The naive write overwrote it, so the US table expanded to 12 dates from 13 rules and "Christmas Eve" vanished from the log. Fix: an observed date never overwrites an occupied one; the rule that owns a date naturally keeps the name. The day is a holiday either way, so only the logged name was ever at risk.

**The cross-year merge needed an order.** Expanding `Y-1, Y, Y+1` and filtering to `Y` reintroduced the same clobber across the boundary: New Year's Day 2028 (a Saturday) shifts back onto 31 December 2027, which the New Year's Eve rule already held. Fix: merge the requested year first, then let a neighbour's observed date fill only a day nothing already claims.

**"Every rule gets its own date" is not a real invariant.** The first version of the table test asserted rule count equals expanded-date count, and it failed honestly: in 2027 Norwegian Whit Monday (Easter +50) lands on 17 May, which is also Grunnlovsdag. Two genuine holidays, one date. Real calendars collide. The invariant was reframed to what actually matters — every rule whose natural date is a weekday must make that day a holiday — and now runs across all 19 countries for 2026–2035. Weekend-landing rules need no assertion, since the weekend check covers them regardless.

## Tests as built

- `golden_test.go` pins the **complete** 2027 expansion for all 19 countries in both directions: no expected date missing, no unexpected date present. Roughly 230 dates, each checked by hand against the country's published calendar. A typo in any month, day, or Easter offset surfaces as a concrete date diff.
- `TestSupportedCountries_EveryWeekdayRuleIsAHoliday` runs the reframed invariant over 2026–2035.
- `TestGregorianEaster` asserts 1900, 2000, 2026–2031, 2100, and 2200, and that every result is a Sunday.
- `TestScheduler_SkipLogFields` pins the `skipped digest` line's fields, because `docs/troubleshooting.md` tells operators to grep for them.
- `TestModule_UnknownCountryFailsStartup` proves a bad code aborts the fx graph rather than silently disabling holidays.

## Acceptance criteria

Mapped to the issue, including the one that changed:

- [x] Digest does not post Saturday or Sunday in the configured digest timezone — via the unconditional weekend check, not a cron spec.
- [x] Digest does not post Dec 25 and Dec 31 — for any configured country; both are in every table. **Without `digest.country` set, holidays are not skipped** and a startup warning says so. This narrows the issue's original wording, deliberately.
- [x] Skips logged with a reason; no Slack call is made — the check runs in the scheduler, before the job.
- [x] Covered by tests, including timezone edge cases around midnight.
- [x] Docs updated.

Not delivered, and why: `digest.skip_dates` for company-specific days (rejected in favour of country data only); regional subdivisions (`DE-BY`); per-repo `country`; a configurable weekend toggle.
