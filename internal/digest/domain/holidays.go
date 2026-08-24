package domain

import (
	"sort"
	"time"
)

// HolidayRule is one entry of a country's holiday table: how the date is
// derived (Kind) and what happens when it lands on a weekend (Observance).
// Only the fields relevant to Kind are set; the rest stay zero.
type HolidayRule struct {
	Name string
	Kind RuleKind

	// Month applies to RuleFixed, RuleNthWeekday, and RuleWeekdayOnOrAfter.
	Month time.Month
	// Day applies to RuleFixed and, as the anchor, to RuleWeekdayOnOrAfter.
	Day int
	// EasterDays applies to RuleEasterOffset.
	EasterDays int
	// Weekday applies to RuleNthWeekday and RuleWeekdayOnOrAfter.
	Weekday time.Weekday
	// Nth applies to RuleNthWeekday: 1..5, or -1 for the last in the month.
	Nth int

	Observance ObservanceKind
}

// Dec 24 and Dec 31 appear in every table. They are not legal public holidays
// in most of these countries, but they are de-facto shutdown days across
// engineering teams, and a digest nobody reads is worse than no digest. Where a
// country has since made one of them statutory (Poland's Wigilia, 2025) the
// table comment says so.

func fixed(name string, month time.Month, day int, observance ObservanceKind) HolidayRule {
	return HolidayRule{Name: name, Kind: RuleFixed, Month: month, Day: day, Observance: observance}
}

func easter(name string, offset int) HolidayRule {
	return HolidayRule{Name: name, Kind: RuleEasterOffset, EasterDays: offset}
}

func nth(name string, nth int, weekday time.Weekday, month time.Month) HolidayRule {
	return HolidayRule{Name: name, Kind: RuleNthWeekday, Nth: nth, Weekday: weekday, Month: month}
}

func onOrAfter(name string, weekday time.Weekday, month time.Month, day int) HolidayRule {
	return HolidayRule{Name: name, Kind: RuleWeekdayOnOrAfter, Weekday: weekday, Month: month, Day: day}
}

// Rules that always land on a weekend are deliberately absent from every table:
// Easter Sunday, Pentecost Sunday, Swedish Midsommardagen and Alla helgons dag,
// Finnish Juhannuspäivä and Pyhäinpäivä. The weekend check already covers them,
// so listing them would be dead data.

// holidaysUS is the US federal set. Every fixed date carries the federal
// observance rule (Saturday to the preceding Friday, Sunday to the following
// Monday); the Nth-weekday holidays name their weekday outright.
var holidaysUS = []HolidayRule{
	fixed("New Year's Day", time.January, 1, ObserveUSShift),
	nth("Martin Luther King Jr. Day", 3, time.Monday, time.January),
	nth("Washington's Birthday", 3, time.Monday, time.February),
	nth("Memorial Day", -1, time.Monday, time.May),
	fixed("Juneteenth", time.June, 19, ObserveUSShift),
	fixed("Independence Day", time.July, 4, ObserveUSShift),
	nth("Labor Day", 1, time.Monday, time.September),
	nth("Columbus Day", 2, time.Monday, time.October),
	fixed("Veterans Day", time.November, 11, ObserveUSShift),
	nth("Thanksgiving", 4, time.Thursday, time.November),
	fixed("Christmas Eve", time.December, 24, ObserveUSShift),
	fixed("Christmas Day", time.December, 25, ObserveUSShift),
	fixed("New Year's Eve", time.December, 31, ObserveUSShift),
}

// holidaysGB covers England and Wales. Scotland (2 January, 30 November) and
// Northern Ireland (17 March, 12 July) differ; those need subdivision support.
var holidaysGB = []HolidayRule{
	fixed("New Year's Day", time.January, 1, ObserveUKSubstitute),
	easter("Good Friday", -2),
	easter("Easter Monday", 1),
	nth("Early May bank holiday", 1, time.Monday, time.May),
	nth("Spring bank holiday", -1, time.Monday, time.May),
	nth("Summer bank holiday", -1, time.Monday, time.August),
	fixed("Christmas Eve", time.December, 24, ObserveUKSubstitute),
	fixed("Christmas Day", time.December, 25, ObserveUKSubstitute),
	fixed("Boxing Day", time.December, 26, ObserveUKSubstitute),
	fixed("New Year's Eve", time.December, 31, ObserveUKSubstitute),
}

// holidaysIE omits Good Friday, which is a bank holiday in Ireland but not a
// statutory public holiday. St Brigid's Day is modelled as the first Monday in
// February; the statutory rule is "1 February when it falls on a Friday,
// otherwise the first Monday", so this is wrong in years where 1 February is a
// Friday (2030, 2036).
var holidaysIE = []HolidayRule{
	fixed("New Year's Day", time.January, 1, ObserveUKSubstitute),
	nth("St Brigid's Day", 1, time.Monday, time.February),
	fixed("St Patrick's Day", time.March, 17, ObserveUKSubstitute),
	easter("Easter Monday", 1),
	nth("May Day", 1, time.Monday, time.May),
	nth("June holiday", 1, time.Monday, time.June),
	nth("August holiday", 1, time.Monday, time.August),
	nth("October holiday", -1, time.Monday, time.October),
	fixed("Christmas Eve", time.December, 24, ObserveUKSubstitute),
	fixed("Christmas Day", time.December, 25, ObserveUKSubstitute),
	fixed("St Stephen's Day", time.December, 26, ObserveUKSubstitute),
	fixed("New Year's Eve", time.December, 31, ObserveUKSubstitute),
}

// holidaysDE is the nine federal holidays. Bundesland-only days (Heilige Drei
// Könige, Fronleichnam, Mariä Himmelfahrt, Reformationstag, Allerheiligen,
// Buß- und Bettag) need subdivision support and are excluded.
var holidaysDE = []HolidayRule{
	fixed("Neujahr", time.January, 1, ObserveNone),
	easter("Karfreitag", -2),
	easter("Ostermontag", 1),
	fixed("Tag der Arbeit", time.May, 1, ObserveNone),
	easter("Christi Himmelfahrt", 39),
	easter("Pfingstmontag", 50),
	fixed("Tag der Deutschen Einheit", time.October, 3, ObserveNone),
	fixed("Heiligabend", time.December, 24, ObserveNone),
	fixed("1. Weihnachtstag", time.December, 25, ObserveNone),
	fixed("2. Weihnachtstag", time.December, 26, ObserveNone),
	fixed("Silvester", time.December, 31, ObserveNone),
}

// holidaysAT omits Good Friday, which stopped being an Austrian public holiday
// in 2019.
var holidaysAT = []HolidayRule{
	fixed("Neujahr", time.January, 1, ObserveNone),
	fixed("Heilige Drei Könige", time.January, 6, ObserveNone),
	easter("Ostermontag", 1),
	fixed("Staatsfeiertag", time.May, 1, ObserveNone),
	easter("Christi Himmelfahrt", 39),
	easter("Pfingstmontag", 50),
	easter("Fronleichnam", 60),
	fixed("Mariä Himmelfahrt", time.August, 15, ObserveNone),
	fixed("Nationalfeiertag", time.October, 26, ObserveNone),
	fixed("Allerheiligen", time.November, 1, ObserveNone),
	fixed("Mariä Empfängnis", time.December, 8, ObserveNone),
	fixed("Heiligabend", time.December, 24, ObserveNone),
	fixed("Christtag", time.December, 25, ObserveNone),
	fixed("Stefanitag", time.December, 26, ObserveNone),
	fixed("Silvester", time.December, 31, ObserveNone),
}

// holidaysCH is approximate by necessity: only 1 August is a federal holiday
// and everything else is cantonal. This is the set observed across most
// German-speaking cantons; Geneva, Ticino, and Valais differ materially.
var holidaysCH = []HolidayRule{
	fixed("Neujahr", time.January, 1, ObserveNone),
	easter("Karfreitag", -2),
	easter("Ostermontag", 1),
	easter("Auffahrt", 39),
	easter("Pfingstmontag", 50),
	fixed("Bundesfeier", time.August, 1, ObserveNone),
	fixed("Heiligabend", time.December, 24, ObserveNone),
	fixed("Weihnachten", time.December, 25, ObserveNone),
	fixed("Stephanstag", time.December, 26, ObserveNone),
	fixed("Silvester", time.December, 31, ObserveNone),
}

// holidaysNL includes Goede Vrijdag, which is a national holiday but not a
// mandatory day off — many Dutch employers work it. Bevrijdingsdag (5 May) is a
// general day off only every fifth year and is omitted.
var holidaysNL = []HolidayRule{
	fixed("Nieuwjaarsdag", time.January, 1, ObserveNone),
	easter("Goede Vrijdag", -2),
	easter("Tweede Paasdag", 1),
	fixed("Koningsdag", time.April, 27, ObserveNone),
	easter("Hemelvaartsdag", 39),
	easter("Tweede Pinksterdag", 50),
	fixed("Kerstavond", time.December, 24, ObserveNone),
	fixed("Eerste Kerstdag", time.December, 25, ObserveNone),
	fixed("Tweede Kerstdag", time.December, 26, ObserveNone),
	fixed("Oudejaarsdag", time.December, 31, ObserveNone),
}

var holidaysBE = []HolidayRule{
	fixed("Nieuwjaar", time.January, 1, ObserveNone),
	easter("Paasmaandag", 1),
	fixed("Dag van de Arbeid", time.May, 1, ObserveNone),
	easter("Onze-Lieve-Heer-Hemelvaart", 39),
	easter("Pinkstermaandag", 50),
	fixed("Nationale feestdag", time.July, 21, ObserveNone),
	fixed("Onze-Lieve-Vrouw-Hemelvaart", time.August, 15, ObserveNone),
	fixed("Allerheiligen", time.November, 1, ObserveNone),
	fixed("Wapenstilstand", time.November, 11, ObserveNone),
	fixed("Kerstavond", time.December, 24, ObserveNone),
	fixed("Kerstmis", time.December, 25, ObserveNone),
	fixed("Oudejaar", time.December, 31, ObserveNone),
}

var holidaysLU = []HolidayRule{
	fixed("Neijoerschdag", time.January, 1, ObserveNone),
	easter("Ouschterméindag", 1),
	fixed("Dag vun der Aarbecht", time.May, 1, ObserveNone),
	fixed("Europadag", time.May, 9, ObserveNone),
	easter("Christi Himmelfaart", 39),
	easter("Péngschtméindag", 50),
	fixed("Nationalfeierdag", time.June, 23, ObserveNone),
	fixed("Mariä Himmelfaart", time.August, 15, ObserveNone),
	fixed("Allerhellgen", time.November, 1, ObserveNone),
	fixed("Hellegabend", time.December, 24, ObserveNone),
	fixed("Chrëschtdag", time.December, 25, ObserveNone),
	fixed("Stefansdag", time.December, 26, ObserveNone),
	fixed("Silvester", time.December, 31, ObserveNone),
}

// holidaysFR is the national set. Alsace-Moselle additionally observes Good
// Friday and 26 December; those need subdivision support.
var holidaysFR = []HolidayRule{
	fixed("Jour de l'An", time.January, 1, ObserveNone),
	easter("Lundi de Pâques", 1),
	fixed("Fête du Travail", time.May, 1, ObserveNone),
	fixed("Victoire 1945", time.May, 8, ObserveNone),
	easter("Ascension", 39),
	easter("Lundi de Pentecôte", 50),
	fixed("Fête nationale", time.July, 14, ObserveNone),
	fixed("Assomption", time.August, 15, ObserveNone),
	fixed("Toussaint", time.November, 1, ObserveNone),
	fixed("Armistice", time.November, 11, ObserveNone),
	fixed("Réveillon de Noël", time.December, 24, ObserveNone),
	fixed("Noël", time.December, 25, ObserveNone),
	fixed("Saint-Sylvestre", time.December, 31, ObserveNone),
}

// holidaysES is the national set. Regional days — Maundy Thursday in most
// autonomous communities, regional fiestas — need subdivision support.
var holidaysES = []HolidayRule{
	fixed("Año Nuevo", time.January, 1, ObserveNone),
	fixed("Epifanía del Señor", time.January, 6, ObserveNone),
	easter("Viernes Santo", -2),
	fixed("Día del Trabajador", time.May, 1, ObserveNone),
	fixed("Asunción de la Virgen", time.August, 15, ObserveNone),
	fixed("Fiesta Nacional", time.October, 12, ObserveNone),
	fixed("Todos los Santos", time.November, 1, ObserveNone),
	fixed("Día de la Constitución", time.December, 6, ObserveNone),
	fixed("Inmaculada Concepción", time.December, 8, ObserveNone),
	fixed("Nochebuena", time.December, 24, ObserveNone),
	fixed("Navidad", time.December, 25, ObserveNone),
	fixed("Nochevieja", time.December, 31, ObserveNone),
}

// holidaysPT omits Carnival (Easter -47), which is not a statutory national
// holiday even though many employers grant it.
var holidaysPT = []HolidayRule{
	fixed("Ano Novo", time.January, 1, ObserveNone),
	easter("Sexta-feira Santa", -2),
	fixed("Dia da Liberdade", time.April, 25, ObserveNone),
	fixed("Dia do Trabalhador", time.May, 1, ObserveNone),
	easter("Corpo de Deus", 60),
	fixed("Dia de Portugal", time.June, 10, ObserveNone),
	fixed("Assunção de Nossa Senhora", time.August, 15, ObserveNone),
	fixed("Implantação da República", time.October, 5, ObserveNone),
	fixed("Todos os Santos", time.November, 1, ObserveNone),
	fixed("Restauração da Independência", time.December, 1, ObserveNone),
	fixed("Imaculada Conceição", time.December, 8, ObserveNone),
	fixed("Véspera de Natal", time.December, 24, ObserveNone),
	fixed("Natal", time.December, 25, ObserveNone),
	fixed("Véspera de Ano Novo", time.December, 31, ObserveNone),
}

var holidaysIT = []HolidayRule{
	fixed("Capodanno", time.January, 1, ObserveNone),
	fixed("Epifania", time.January, 6, ObserveNone),
	easter("Lunedì dell'Angelo", 1),
	fixed("Festa della Liberazione", time.April, 25, ObserveNone),
	fixed("Festa del Lavoro", time.May, 1, ObserveNone),
	fixed("Festa della Repubblica", time.June, 2, ObserveNone),
	fixed("Ferragosto", time.August, 15, ObserveNone),
	fixed("Ognissanti", time.November, 1, ObserveNone),
	fixed("Immacolata Concezione", time.December, 8, ObserveNone),
	fixed("Vigilia di Natale", time.December, 24, ObserveNone),
	fixed("Natale", time.December, 25, ObserveNone),
	fixed("Santo Stefano", time.December, 26, ObserveNone),
	fixed("San Silvestro", time.December, 31, ObserveNone),
}

// holidaysSE includes Midsommarafton, Julafton, and Nyårsafton, which are not
// statutory in Sweden but are near-universal days off.
var holidaysSE = []HolidayRule{
	fixed("Nyårsdagen", time.January, 1, ObserveNone),
	fixed("Trettondedag jul", time.January, 6, ObserveNone),
	easter("Långfredagen", -2),
	easter("Annandag påsk", 1),
	fixed("Första maj", time.May, 1, ObserveNone),
	easter("Kristi himmelsfärdsdag", 39),
	fixed("Sveriges nationaldag", time.June, 6, ObserveNone),
	onOrAfter("Midsommarafton", time.Friday, time.June, 19),
	fixed("Julafton", time.December, 24, ObserveNone),
	fixed("Juldagen", time.December, 25, ObserveNone),
	fixed("Annandag jul", time.December, 26, ObserveNone),
	fixed("Nyårsafton", time.December, 31, ObserveNone),
}

var holidaysNO = []HolidayRule{
	fixed("Første nyttårsdag", time.January, 1, ObserveNone),
	easter("Skjærtorsdag", -3),
	easter("Langfredag", -2),
	easter("Andre påskedag", 1),
	fixed("Arbeidernes dag", time.May, 1, ObserveNone),
	fixed("Grunnlovsdag", time.May, 17, ObserveNone),
	easter("Kristi himmelfartsdag", 39),
	easter("Andre pinsedag", 50),
	fixed("Julaften", time.December, 24, ObserveNone),
	fixed("Første juledag", time.December, 25, ObserveNone),
	fixed("Andre juledag", time.December, 26, ObserveNone),
	fixed("Nyttårsaften", time.December, 31, ObserveNone),
}

// holidaysDK deliberately omits Store bededag, abolished as a Danish public
// holiday from 2024, and Grundlovsdag (5 June), which is not statutory even
// though many employers grant it.
var holidaysDK = []HolidayRule{
	fixed("Nytårsdag", time.January, 1, ObserveNone),
	easter("Skærtorsdag", -3),
	easter("Langfredag", -2),
	easter("2. påskedag", 1),
	easter("Kristi himmelfartsdag", 39),
	easter("2. pinsedag", 50),
	fixed("Juleaftensdag", time.December, 24, ObserveNone),
	fixed("1. juledag", time.December, 25, ObserveNone),
	fixed("2. juledag", time.December, 26, ObserveNone),
	fixed("Nytårsaftensdag", time.December, 31, ObserveNone),
}

var holidaysFI = []HolidayRule{
	fixed("Uudenvuodenpäivä", time.January, 1, ObserveNone),
	fixed("Loppiainen", time.January, 6, ObserveNone),
	easter("Pitkäperjantai", -2),
	easter("2. pääsiäispäivä", 1),
	fixed("Vappu", time.May, 1, ObserveNone),
	easter("Helatorstai", 39),
	onOrAfter("Juhannusaatto", time.Friday, time.June, 19),
	fixed("Itsenäisyyspäivä", time.December, 6, ObserveNone),
	fixed("Jouluaatto", time.December, 24, ObserveNone),
	fixed("Joulupäivä", time.December, 25, ObserveNone),
	fixed("Tapaninpäivä", time.December, 26, ObserveNone),
	fixed("Uudenvuodenaatto", time.December, 31, ObserveNone),
}

// holidaysPL: Wigilia (24 December) became a statutory Polish public holiday in
// 2025, so it is a real holiday here rather than a de-facto one.
var holidaysPL = []HolidayRule{
	fixed("Nowy Rok", time.January, 1, ObserveNone),
	fixed("Święto Trzech Króli", time.January, 6, ObserveNone),
	easter("Poniedziałek Wielkanocny", 1),
	fixed("Święto Pracy", time.May, 1, ObserveNone),
	fixed("Święto Konstytucji 3 Maja", time.May, 3, ObserveNone),
	easter("Boże Ciało", 60),
	fixed("Wniebowzięcie NMP", time.August, 15, ObserveNone),
	fixed("Wszystkich Świętych", time.November, 1, ObserveNone),
	fixed("Święto Niepodległości", time.November, 11, ObserveNone),
	fixed("Wigilia Bożego Narodzenia", time.December, 24, ObserveNone),
	fixed("Boże Narodzenie", time.December, 25, ObserveNone),
	fixed("Drugi dzień Bożego Narodzenia", time.December, 26, ObserveNone),
	fixed("Sylwester", time.December, 31, ObserveNone),
}

// holidaysUA carries fixed dates only. Pascha and Trinity Sunday need Julian
// Easter arithmetic, and both fall on a Sunday regardless — already covered by
// the weekend check — so only Easter Monday would ever have differed. Martial
// law currently suspends Ukrainian public holidays entirely, which no calendar
// can model.
var holidaysUA = []HolidayRule{
	fixed("Новий рік", time.January, 1, ObserveNone),
	fixed("Міжнародний жіночий день", time.March, 8, ObserveNone),
	fixed("День праці", time.May, 1, ObserveNone),
	fixed("День Конституції", time.June, 28, ObserveNone),
	fixed("День Незалежності", time.August, 24, ObserveNone),
	fixed("День захисників і захисниць", time.October, 1, ObserveNone),
	fixed("Святвечір", time.December, 24, ObserveNone),
	fixed("Різдво", time.December, 25, ObserveNone),
	fixed("Новорічний вечір", time.December, 31, ObserveNone),
}

// holidayTables maps each supported country to its rule table. A code absent
// from this map is rejected at startup.
var holidayTables = map[CountryCode][]HolidayRule{
	CountryAustria:       holidaysAT,
	CountryBelgium:       holidaysBE,
	CountryDenmark:       holidaysDK,
	CountryFinland:       holidaysFI,
	CountryFrance:        holidaysFR,
	CountryGermany:       holidaysDE,
	CountryIreland:       holidaysIE,
	CountryItaly:         holidaysIT,
	CountryLuxembourg:    holidaysLU,
	CountryNetherlands:   holidaysNL,
	CountryNorway:        holidaysNO,
	CountryPoland:        holidaysPL,
	CountryPortugal:      holidaysPT,
	CountrySpain:         holidaysES,
	CountrySweden:        holidaysSE,
	CountrySwitzerland:   holidaysCH,
	CountryUkraine:       holidaysUA,
	CountryUnitedKingdom: holidaysGB,
	CountryUnitedStates:  holidaysUS,
}

// HolidayTable returns the rule table for a country code, and whether the code
// is supported.
func HolidayTable(code CountryCode) ([]HolidayRule, bool) {
	rules, ok := holidayTables[code]
	return rules, ok
}

// SupportedCountries returns every supported country code, sorted, for error
// messages and documentation.
func SupportedCountries() []CountryCode {
	codes := make([]CountryCode, 0, len(holidayTables))
	for code := range holidayTables {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}
