package infrastructure_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	digestapp "github.com/mptooling/notifycat/internal/digest/application"
	digestdomain "github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/feed/infrastructure"
)

func TestDigestDayOffCalendar_IsDayOff(t *testing.T) {
	testCases := []struct {
		name    string
		country string
		moment  time.Time
		want    bool
	}{
		{"weekday", "", time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), false},
		{"saturday", "", time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC), true},
		{"public holiday without a country", "", time.Date(2026, 12, 25, 10, 0, 0, 0, time.UTC), false},
		{"public holiday", "DE", time.Date(2026, 12, 25, 10, 0, 0, 0, time.UTC), true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calendar := infrastructure.NewDigestDayOffCalendar(digestapp.NewCalendar(digestdomain.CalendarParams{Country: testCase.country}))

			assert.Equal(t, testCase.want, calendar.IsDayOff(testCase.moment))
		})
	}
}
