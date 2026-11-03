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

// IsDayOff implements WorkCalendar.
func (calendar *WorkCalendar) IsDayOff(moment time.Time) bool {
	return calendar.daysOff.IsDayOff(moment.In(calendar.location))
}

// Day implements WorkCalendar.
func (calendar *WorkCalendar) Day(moment time.Time) string {
	return moment.In(calendar.location).Format(domain.DayLayout)
}

// AddWorkingTime implements WorkCalendar.
func (calendar *WorkCalendar) AddWorkingTime(start time.Time, workingTime time.Duration) time.Time {
	cursor := start.In(calendar.location)
	remaining := workingTime
	for range domain.MaxWorkingTimeSearchDays {
		nextMidnight := time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, calendar.location)
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
