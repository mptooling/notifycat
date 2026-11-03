package infrastructure

import (
	"time"

	digestdomain "github.com/mptooling/notifycat/internal/digest/domain"
)

// DigestDayOffCalendar reuses the digest calendar's weekends and public holidays as the feed's days off.
type DigestDayOffCalendar struct {
	digestCalendar digestdomain.DigestCalendar
}

// NewDigestDayOffCalendar builds a DigestDayOffCalendar.
func NewDigestDayOffCalendar(digestCalendar digestdomain.DigestCalendar) *DigestDayOffCalendar {
	return &DigestDayOffCalendar{digestCalendar: digestCalendar}
}

// IsDayOff implements domain.DayOffCalendar.
func (calendar *DigestDayOffCalendar) IsDayOff(localMoment time.Time) bool {
	_, skip := calendar.digestCalendar.SkipReason(localMoment)
	return skip
}
