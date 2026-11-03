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

			assert.WithinDuration(t, testCase.want, got, 0)
		})
	}
}

func TestWorkCalendar_AddWorkingTimeUsesTheLocation(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}, Location: berlin})
	fridayNightBerlin := utc(2026, 11, 6, 22, 50)

	got := calendar.AddWorkingTime(fridayNightBerlin, 30*time.Minute)

	assert.WithinDuration(t, utc(2026, 11, 8, 23, 20), got, 0)
}

func TestWorkCalendar_AddWorkingTimeAcrossASkippedLocalMidnight(t *testing.T) {
	santiago, err := time.LoadLocation("America/Santiago")
	require.NoError(t, err)
	testCases := []struct {
		name        string
		workingTime time.Duration
		want        time.Time
	}{
		{"short time", 30 * time.Minute, time.Date(2026, 9, 7, 0, 20, 0, 0, santiago)},
		{"a full day", 24 * time.Hour, time.Date(2026, 9, 7, 23, 50, 0, 0, santiago)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}, Location: santiago})
			fridayNight := time.Date(2026, 9, 4, 23, 50, 0, 0, santiago)

			got := calendar.AddWorkingTime(fridayNight, testCase.workingTime)

			assert.WithinDuration(t, testCase.want, got, 0)
		})
	}
}

func TestWorkCalendar_AddWorkingTimeAcrossACairoClockChange(t *testing.T) {
	cairo, err := time.LoadLocation("Africa/Cairo")
	require.NoError(t, err)
	testCases := []struct {
		name        string
		start       time.Time
		workingTime time.Duration
		want        time.Time
	}{
		{"friday 2026-04-24 starts at 01:00 and has 23 hours", utc(2026, 4, 23, 10, 0), 36 * time.Hour, utc(2026, 4, 26, 22, 0)},
		{"thursday 2026-10-29 repeats 23:00 and has 25 hours", utc(2026, 10, 29, 9, 0), 13*time.Hour + 30*time.Minute, utc(2026, 10, 29, 22, 30)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}, Location: cairo})

			got := calendar.AddWorkingTime(testCase.start, testCase.workingTime)

			assert.WithinDuration(t, testCase.want, got, 0)
		})
	}
}

func TestWorkCalendar_IsDayOffUsesTheLocation(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}, Location: berlin})
	saturdayEarlyBerlin := utc(2026, 11, 6, 23, 30)

	got := calendar.IsDayOff(saturdayEarlyBerlin)

	assert.True(t, got)
}

func TestWorkCalendar_DayUsesTheLocation(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}, Location: berlin})
	saturdayEarlyBerlin := utc(2026, 11, 6, 23, 30)

	got := calendar.Day(saturdayEarlyBerlin)

	assert.Equal(t, "2026-11-07", got)
}

func TestWorkCalendar_NilLocationIsUTC(t *testing.T) {
	calendar := application.NewWorkCalendar(domain.WorkCalendarParams{DaysOff: daysOffFake{}})
	saturdayUTC := utc(2026, 11, 7, 0, 30)

	isDayOff := calendar.IsDayOff(saturdayUTC)
	day := calendar.Day(saturdayUTC)

	assert.True(t, isDayOff)
	assert.Equal(t, "2026-11-07", day)
}
