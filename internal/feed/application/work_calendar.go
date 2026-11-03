package application

import (
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

// WorkCalendar implements domain.WorkCalendar over a day-off calendar.
type WorkCalendar struct {
	daysOff  domain.DayOffCalendar
	location *time.Location
}

// NewWorkCalendar builds a WorkCalendar; a nil location means UTC.
func NewWorkCalendar(params domain.WorkCalendarParams) *WorkCalendar {
	location := params.Location
	if location == nil {
		location = time.UTC
	}
	return &WorkCalendar{daysOff: params.DaysOff, location: location}
}

// IsDayOff implements domain.WorkCalendar.
func (calendar *WorkCalendar) IsDayOff(moment time.Time) bool {
	return calendar.daysOff.IsDayOff(moment.In(calendar.location))
}

// Day implements domain.WorkCalendar.
func (calendar *WorkCalendar) Day(moment time.Time) string {
	return moment.In(calendar.location).Format(domain.DayLayout)
}

// AddWorkingTime implements domain.WorkCalendar.
func (calendar *WorkCalendar) AddWorkingTime(start time.Time, workingTime time.Duration) time.Time {
	cursor := start.In(calendar.location)
	remaining := workingTime
	for range domain.MaxWorkingTimeSearchDays {
		nextMidnight := startOfNextDay(cursor)
		if calendar.daysOff.IsDayOff(cursor) {
			cursor = nextMidnight
			continue
		}
		untilMidnight := nextMidnight.Sub(cursor)
		if remaining <= untilMidnight {
			return cursor.Add(remaining)
		}
		remaining -= untilMidnight
		cursor = nextMidnight
	}
	return start.Add(workingTime)
}

func startOfNextDay(moment time.Time) time.Time {
	nextDay := time.Date(moment.Year(), moment.Month(), moment.Day()+1, 0, 0, 0, 0, moment.Location())
	if nextDay.Day() == moment.Day() {
		_, nextDay = nextDay.ZoneBounds()
	}
	return nextDay
}
