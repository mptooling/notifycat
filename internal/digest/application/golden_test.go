package application

import (
	"sort"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
)

// golden2027 is the fully expanded holiday set for each supported country in
// 2027 — the stress year: Christmas falls on a Saturday and Boxing Day on a
// Sunday, so UK-style substitutes chain and US-style shifts collide with the
// de-facto Christmas Eve entry. Every date here was checked by hand against
// the country's published calendar. Dates falling on a weekend are included
// for completeness even though the weekend check would skip them anyway.
var golden2027 = map[domain.CountryCode]map[string]string{
	"AT": {
		"2027-01-01": "Neujahr",             // Fri
		"2027-01-06": "Heilige Drei Könige", // Wed
		"2027-03-29": "Ostermontag",         // Mon
		"2027-05-01": "Staatsfeiertag",      // Sat
		"2027-05-06": "Christi Himmelfahrt", // Thu
		"2027-05-17": "Pfingstmontag",       // Mon
		"2027-05-27": "Fronleichnam",        // Thu
		"2027-08-15": "Mariä Himmelfahrt",   // Sun
		"2027-10-26": "Nationalfeiertag",    // Tue
		"2027-11-01": "Allerheiligen",       // Mon
		"2027-12-08": "Mariä Empfängnis",    // Wed
		"2027-12-24": "Heiligabend",         // Fri
		"2027-12-25": "Christtag",           // Sat
		"2027-12-26": "Stefanitag",          // Sun
		"2027-12-31": "Silvester",           // Fri
	},
	"BE": {
		"2027-01-01": "Nieuwjaar",                   // Fri
		"2027-03-29": "Paasmaandag",                 // Mon
		"2027-05-01": "Dag van de Arbeid",           // Sat
		"2027-05-06": "Onze-Lieve-Heer-Hemelvaart",  // Thu
		"2027-05-17": "Pinkstermaandag",             // Mon
		"2027-07-21": "Nationale feestdag",          // Wed
		"2027-08-15": "Onze-Lieve-Vrouw-Hemelvaart", // Sun
		"2027-11-01": "Allerheiligen",               // Mon
		"2027-11-11": "Wapenstilstand",              // Thu
		"2027-12-24": "Kerstavond",                  // Fri
		"2027-12-25": "Kerstmis",                    // Sat
		"2027-12-31": "Oudejaar",                    // Fri
	},
	"CH": {
		"2027-01-01": "Neujahr",       // Fri
		"2027-03-26": "Karfreitag",    // Fri
		"2027-03-29": "Ostermontag",   // Mon
		"2027-05-06": "Auffahrt",      // Thu
		"2027-05-17": "Pfingstmontag", // Mon
		"2027-08-01": "Bundesfeier",   // Sun
		"2027-12-24": "Heiligabend",   // Fri
		"2027-12-25": "Weihnachten",   // Sat
		"2027-12-26": "Stephanstag",   // Sun
		"2027-12-31": "Silvester",     // Fri
	},
	"DE": {
		"2027-01-01": "Neujahr",                   // Fri
		"2027-03-26": "Karfreitag",                // Fri
		"2027-03-29": "Ostermontag",               // Mon
		"2027-05-01": "Tag der Arbeit",            // Sat
		"2027-05-06": "Christi Himmelfahrt",       // Thu
		"2027-05-17": "Pfingstmontag",             // Mon
		"2027-10-03": "Tag der Deutschen Einheit", // Sun
		"2027-12-24": "Heiligabend",               // Fri
		"2027-12-25": "1. Weihnachtstag",          // Sat
		"2027-12-26": "2. Weihnachtstag",          // Sun
		"2027-12-31": "Silvester",                 // Fri
	},
	"DK": {
		"2027-01-01": "Nytårsdag",             // Fri
		"2027-03-25": "Skærtorsdag",           // Thu
		"2027-03-26": "Langfredag",            // Fri
		"2027-03-29": "2. påskedag",           // Mon
		"2027-05-06": "Kristi himmelfartsdag", // Thu
		"2027-05-17": "2. pinsedag",           // Mon
		"2027-12-24": "Juleaftensdag",         // Fri
		"2027-12-25": "1. juledag",            // Sat
		"2027-12-26": "2. juledag",            // Sun
		"2027-12-31": "Nytårsaftensdag",       // Fri
	},
	"ES": {
		"2027-01-01": "Año Nuevo",              // Fri
		"2027-01-06": "Epifanía del Señor",     // Wed
		"2027-03-26": "Viernes Santo",          // Fri
		"2027-05-01": "Día del Trabajador",     // Sat
		"2027-08-15": "Asunción de la Virgen",  // Sun
		"2027-10-12": "Fiesta Nacional",        // Tue
		"2027-11-01": "Todos los Santos",       // Mon
		"2027-12-06": "Día de la Constitución", // Mon
		"2027-12-08": "Inmaculada Concepción",  // Wed
		"2027-12-24": "Nochebuena",             // Fri
		"2027-12-25": "Navidad",                // Sat
		"2027-12-31": "Nochevieja",             // Fri
	},
	"FI": {
		"2027-01-01": "Uudenvuodenpäivä", // Fri
		"2027-01-06": "Loppiainen",       // Wed
		"2027-03-26": "Pitkäperjantai",   // Fri
		"2027-03-29": "2. pääsiäispäivä", // Mon
		"2027-05-01": "Vappu",            // Sat
		"2027-05-06": "Helatorstai",      // Thu
		"2027-06-25": "Juhannusaatto",    // Fri
		"2027-12-06": "Itsenäisyyspäivä", // Mon
		"2027-12-24": "Jouluaatto",       // Fri
		"2027-12-25": "Joulupäivä",       // Sat
		"2027-12-26": "Tapaninpäivä",     // Sun
		"2027-12-31": "Uudenvuodenaatto", // Fri
	},
	"FR": {
		"2027-01-01": "Jour de l'An",       // Fri
		"2027-03-29": "Lundi de Pâques",    // Mon
		"2027-05-01": "Fête du Travail",    // Sat
		"2027-05-06": "Ascension",          // Thu
		"2027-05-08": "Victoire 1945",      // Sat
		"2027-05-17": "Lundi de Pentecôte", // Mon
		"2027-07-14": "Fête nationale",     // Wed
		"2027-08-15": "Assomption",         // Sun
		"2027-11-01": "Toussaint",          // Mon
		"2027-11-11": "Armistice",          // Thu
		"2027-12-24": "Réveillon de Noël",  // Fri
		"2027-12-25": "Noël",               // Sat
		"2027-12-31": "Saint-Sylvestre",    // Fri
	},
	"GB": {
		"2027-01-01": "New Year's Day",         // Fri
		"2027-03-26": "Good Friday",            // Fri
		"2027-03-29": "Easter Monday",          // Mon
		"2027-05-03": "Early May bank holiday", // Mon
		"2027-05-31": "Spring bank holiday",    // Mon
		"2027-08-30": "Summer bank holiday",    // Mon
		"2027-12-24": "Christmas Eve",          // Fri
		"2027-12-27": "Christmas Day",          // Mon
		"2027-12-28": "Boxing Day",             // Tue
		"2027-12-31": "New Year's Eve",         // Fri
	},
	"IE": {
		"2027-01-01": "New Year's Day",   // Fri
		"2027-02-01": "St Brigid's Day",  // Mon
		"2027-03-17": "St Patrick's Day", // Wed
		"2027-03-29": "Easter Monday",    // Mon
		"2027-05-03": "May Day",          // Mon
		"2027-06-07": "June holiday",     // Mon
		"2027-08-02": "August holiday",   // Mon
		"2027-10-25": "October holiday",  // Mon
		"2027-12-24": "Christmas Eve",    // Fri
		"2027-12-27": "Christmas Day",    // Mon
		"2027-12-28": "St Stephen's Day", // Tue
		"2027-12-31": "New Year's Eve",   // Fri
	},
	"IT": {
		"2027-01-01": "Capodanno",               // Fri
		"2027-01-06": "Epifania",                // Wed
		"2027-03-29": "Lunedì dell'Angelo",      // Mon
		"2027-04-25": "Festa della Liberazione", // Sun
		"2027-05-01": "Festa del Lavoro",        // Sat
		"2027-06-02": "Festa della Repubblica",  // Wed
		"2027-08-15": "Ferragosto",              // Sun
		"2027-11-01": "Ognissanti",              // Mon
		"2027-12-08": "Immacolata Concezione",   // Wed
		"2027-12-24": "Vigilia di Natale",       // Fri
		"2027-12-25": "Natale",                  // Sat
		"2027-12-26": "Santo Stefano",           // Sun
		"2027-12-31": "San Silvestro",           // Fri
	},
	"LU": {
		"2027-01-01": "Neijoerschdag",        // Fri
		"2027-03-29": "Ouschterméindag",      // Mon
		"2027-05-01": "Dag vun der Aarbecht", // Sat
		"2027-05-06": "Christi Himmelfaart",  // Thu
		"2027-05-09": "Europadag",            // Sun
		"2027-05-17": "Péngschtméindag",      // Mon
		"2027-06-23": "Nationalfeierdag",     // Wed
		"2027-08-15": "Mariä Himmelfaart",    // Sun
		"2027-11-01": "Allerhellgen",         // Mon
		"2027-12-24": "Hellegabend",          // Fri
		"2027-12-25": "Chrëschtdag",          // Sat
		"2027-12-26": "Stefansdag",           // Sun
		"2027-12-31": "Silvester",            // Fri
	},
	"NL": {
		"2027-01-01": "Nieuwjaarsdag",      // Fri
		"2027-03-26": "Goede Vrijdag",      // Fri
		"2027-03-29": "Tweede Paasdag",     // Mon
		"2027-04-27": "Koningsdag",         // Tue
		"2027-05-06": "Hemelvaartsdag",     // Thu
		"2027-05-17": "Tweede Pinksterdag", // Mon
		"2027-12-24": "Kerstavond",         // Fri
		"2027-12-25": "Eerste Kerstdag",    // Sat
		"2027-12-26": "Tweede Kerstdag",    // Sun
		"2027-12-31": "Oudejaarsdag",       // Fri
	},
	"NO": {
		"2027-01-01": "Første nyttårsdag",     // Fri
		"2027-03-25": "Skjærtorsdag",          // Thu
		"2027-03-26": "Langfredag",            // Fri
		"2027-03-29": "Andre påskedag",        // Mon
		"2027-05-01": "Arbeidernes dag",       // Sat
		"2027-05-06": "Kristi himmelfartsdag", // Thu
		"2027-05-17": "Grunnlovsdag",          // Mon
		"2027-12-24": "Julaften",              // Fri
		"2027-12-25": "Første juledag",        // Sat
		"2027-12-26": "Andre juledag",         // Sun
		"2027-12-31": "Nyttårsaften",          // Fri
	},
	"PL": {
		"2027-01-01": "Nowy Rok",                      // Fri
		"2027-01-06": "Święto Trzech Króli",           // Wed
		"2027-03-29": "Poniedziałek Wielkanocny",      // Mon
		"2027-05-01": "Święto Pracy",                  // Sat
		"2027-05-03": "Święto Konstytucji 3 Maja",     // Mon
		"2027-05-27": "Boże Ciało",                    // Thu
		"2027-08-15": "Wniebowzięcie NMP",             // Sun
		"2027-11-01": "Wszystkich Świętych",           // Mon
		"2027-11-11": "Święto Niepodległości",         // Thu
		"2027-12-24": "Wigilia Bożego Narodzenia",     // Fri
		"2027-12-25": "Boże Narodzenie",               // Sat
		"2027-12-26": "Drugi dzień Bożego Narodzenia", // Sun
		"2027-12-31": "Sylwester",                     // Fri
	},
	"PT": {
		"2027-01-01": "Ano Novo",                     // Fri
		"2027-03-26": "Sexta-feira Santa",            // Fri
		"2027-04-25": "Dia da Liberdade",             // Sun
		"2027-05-01": "Dia do Trabalhador",           // Sat
		"2027-05-27": "Corpo de Deus",                // Thu
		"2027-06-10": "Dia de Portugal",              // Thu
		"2027-08-15": "Assunção de Nossa Senhora",    // Sun
		"2027-10-05": "Implantação da República",     // Tue
		"2027-11-01": "Todos os Santos",              // Mon
		"2027-12-01": "Restauração da Independência", // Wed
		"2027-12-08": "Imaculada Conceição",          // Wed
		"2027-12-24": "Véspera de Natal",             // Fri
		"2027-12-25": "Natal",                        // Sat
		"2027-12-31": "Véspera de Ano Novo",          // Fri
	},
	"SE": {
		"2027-01-01": "Nyårsdagen",             // Fri
		"2027-01-06": "Trettondedag jul",       // Wed
		"2027-03-26": "Långfredagen",           // Fri
		"2027-03-29": "Annandag påsk",          // Mon
		"2027-05-01": "Första maj",             // Sat
		"2027-05-06": "Kristi himmelsfärdsdag", // Thu
		"2027-06-06": "Sveriges nationaldag",   // Sun
		"2027-06-25": "Midsommarafton",         // Fri
		"2027-12-24": "Julafton",               // Fri
		"2027-12-25": "Juldagen",               // Sat
		"2027-12-26": "Annandag jul",           // Sun
		"2027-12-31": "Nyårsafton",             // Fri
	},
	"UA": {
		"2027-01-01": "Новий рік",                   // Fri
		"2027-03-08": "Міжнародний жіночий день",    // Mon
		"2027-05-01": "День праці",                  // Sat
		"2027-06-28": "День Конституції",            // Mon
		"2027-08-24": "День Незалежності",           // Tue
		"2027-10-01": "День захисників і захисниць", // Fri
		"2027-12-24": "Святвечір",                   // Fri
		"2027-12-25": "Різдво",                      // Sat
		"2027-12-31": "Новорічний вечір",            // Fri
	},
	"US": {
		"2027-01-01": "New Year's Day",             // Fri
		"2027-01-18": "Martin Luther King Jr. Day", // Mon
		"2027-02-15": "Washington's Birthday",      // Mon
		"2027-05-31": "Memorial Day",               // Mon
		"2027-06-18": "Juneteenth",                 // Fri
		"2027-07-05": "Independence Day",           // Mon
		"2027-09-06": "Labor Day",                  // Mon
		"2027-10-11": "Columbus Day",               // Mon
		"2027-11-11": "Veterans Day",               // Thu
		"2027-11-25": "Thanksgiving",               // Thu
		"2027-12-24": "Christmas Eve",              // Fri
		"2027-12-31": "New Year's Eve",             // Fri
	},
}

// TestGolden2027 pins every country's full expansion for 2027 in both
// directions: no expected date missing, no unexpected date added. This is the
// regression net under roughly 230 hand-written rules — a typo in a month, day,
// or Easter offset shows up here as a concrete date diff.
func TestGolden2027(t *testing.T) {
	if len(golden2027) != len(domain.SupportedCountries()) {
		t.Fatalf("golden covers %d countries; %d are supported", len(golden2027), len(domain.SupportedCountries()))
	}
	for _, code := range domain.SupportedCountries() {
		want, ok := golden2027[code]
		if !ok {
			t.Errorf("%s: no golden set", code)
			continue
		}
		t.Run(string(code), func(t *testing.T) {
			calendar := newTestCalendar(t, string(code))
			got := calendar.holidaysIn(2027)

			for day, name := range want {
				parsed := date(t, day, time.UTC)
				gotName, isHoliday := got[civil(parsed)]
				if !isHoliday {
					t.Errorf("%s: expected holiday %q, got a working day", day, name)
					continue
				}
				if gotName != name {
					t.Errorf("%s: holiday = %q; want %q", day, gotName, name)
				}
			}

			var unexpected []string
			for day, name := range got {
				if _, expected := want[day.time().Format(time.DateOnly)]; !expected {
					unexpected = append(unexpected, day.time().Format(time.DateOnly)+" "+name)
				}
			}
			sort.Strings(unexpected)
			for _, entry := range unexpected {
				t.Errorf("unexpected holiday: %s", entry)
			}
		})
	}
}
