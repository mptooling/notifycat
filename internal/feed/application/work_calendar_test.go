package application_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/feed/application"
	"github.com/mptooling/notifycat/internal/feed/domain"
)

type daysOffFake struct {
	extraDaysOff map[string]bool
	everyDay     bool
}

func (fake daysOffFake) IsDayOff(localMoment time.Time) bool {
	if fake.everyDay {
		return true
	}
	switch localMoment.Weekday() {
	case time.Saturday, time.Sunday:
		return true
	}
	return fake.extraDaysOff[localMoment.Format(domain.DayLayout)]
}

func utc(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
}

func TestWorkCalendar_AddWorkingTime(t *testing.T) {
	testCases := []struct {
		name        string
		daysOff     daysOffFake
		start       time.Time
		workingTime time.Duration
		want        time.Time
	}{
		{"within a working day", daysOffFake{}, utc(2026, 11, 3, 10, 0), 30 * time.Minute, utc(2026, 11, 3, 10, 30)},
		{"friday night crosses the weekend", daysOffFake{}, utc(2026, 11, 6, 23, 50), 30 * time.Minute, utc(2026, 11, 9, 0, 20)},
		{"start on a day off", daysOffFake{}, utc(2026, 11, 7, 12, 0), time.Hour, utc(2026, 11, 9, 1, 0)},
		{"a full day over the weekend", daysOffFake{}, utc(2026, 11, 6, 20, 0), 24 * time.Hour, utc(2026, 11, 9, 20, 0)},
		{"a weekday off", daysOffFake{extraDaysOff: map[string]bool{"2026-11-04": true}}, utc(2026, 11, 3, 23, 0), 2 * time.Hour, utc(2026, 11, 5, 1, 0)},
		{"every day off hits the search limit", daysOffFake{everyDay: true}, utc(2026, 11, 3, 10, 0), time.Hour, utc(2026, 11, 3, 11, 0)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: testCase.daysOff, Location: time.UTC})

			got := calendar.AddWorkingTime(testCase.start, testCase.workingTime)

			assert.True(t, testCase.want.Equal(got), "want %s got %s", testCase.want, got)
		})
	}
}

func TestWorkCalendar_UsesTheLocation(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}, Location: berlin})
	fridayNightBerlin := utc(2026, 11, 6, 22, 50)
	saturdayEarlyBerlin := utc(2026, 11, 6, 23, 30)

	got := calendar.AddWorkingTime(fridayNightBerlin, 30*time.Minute)

	assert.True(t, utc(2026, 11, 8, 23, 20).Equal(got), "got %s", got)
	assert.True(t, calendar.IsDayOff(saturdayEarlyBerlin))
	assert.Equal(t, "2026-11-07", calendar.Day(saturdayEarlyBerlin))
}

func TestWorkCalendar_NilLocationIsUTC(t *testing.T) {
	calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}})
	saturdayUTC := utc(2026, 11, 7, 0, 30)

	assert.True(t, calendar.IsDayOff(saturdayUTC))
	assert.Equal(t, "2026-11-07", calendar.Day(saturdayUTC))
}
