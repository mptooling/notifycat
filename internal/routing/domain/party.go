package domain

import (
	"fmt"
	"regexp"
	"time"
)

var partyDeadlineRegexp = regexp.MustCompile(PartyDeadlinePattern)

// ParsePartyDeadline parses a party deadline written in hours and minutes.
func ParsePartyDeadline(value string) (time.Duration, error) {
	if value == "" || !partyDeadlineRegexp.MatchString(value) {
		return 0, fmt.Errorf("party deadline %q: use hours and minutes, like 15m, 2h or 1h30m", value)
	}
	deadline, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("party deadline %q: use hours and minutes, like 15m, 2h or 1h30m: %w", value, err)
	}
	if deadline < MinPartyDeadline || deadline > MaxPartyDeadline {
		return 0, fmt.Errorf("party deadline %q: must be from %s", value, PartyDeadlineRangeText)
	}
	return deadline, nil
}

// ValidatePartyReviewers checks that a party needs at least MinPartyReviewers reviewers.
func ValidatePartyReviewers(value int) error {
	if value < MinPartyReviewers {
		return fmt.Errorf("party reviewers %d: must be %d or more", value, MinPartyReviewers)
	}
	return nil
}
