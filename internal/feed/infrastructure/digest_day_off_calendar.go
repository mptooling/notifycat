package infrastructure

import (
	"time"

	digestdomain "github.com/mptooling/notifycat/internal/digest/domain"
)

// DigestDayOffCalendar reuses the digest calendar's weekends and public holidays as the feed's days off.
type DigestDayOffCalendar struct {
	calendar digestdomain.DigestCalendar
}

// NewDigestDayOffCalendar builds a DigestDayOffCalendar.
func NewDigestDayOffCalendar(calendar digestdomain.DigestCalendar) *DigestDayOffCalendar {
	return &DigestDayOffCalendar{calendar: calendar}
}

// IsDayOff implements DayOffCalendar.
func (calendar *DigestDayOffCalendar) IsDayOff(localMoment time.Time) bool {
	_, skip := calendar.calendar.SkipReason(localMoment)
	return skip
}
