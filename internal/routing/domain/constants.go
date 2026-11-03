package domain

import "time"

// DefaultDigestSchedule is the cron spec used when the digest section is absent
// or omits `schedule`: 9am every morning, in the configured digest timezone
// (default UTC; see DigestConfig.Timezone).
const DefaultDigestSchedule = "0 9 * * *"

// HourlyCronSpec is the cron spec of the hourly schedule mode.
const HourlyCronSpec = "0 * * * *"

// AtTimeLayout is the `at:` time format: 24-hour HH:MM.
const AtTimeLayout = "15:04"

// atCronFormat turns one `at:` time into a daily cron spec (minute, hour).
const atCronFormat = "%d %d * * *"

// Party defaults and the allowed range of the party deadline.
const (
	DefaultPartyReviewers = 1
	DefaultPartyDeadline  = time.Hour
	MinPartyDeadline      = 15 * time.Minute
	MaxPartyDeadline      = 24 * time.Hour

	// PartyDeadlineRangeText spells the bounds in the format the parser accepts.
	PartyDeadlineRangeText = "15m to 24h"

	// PartyDeadlinePattern allows hours and minutes only, like 15m, 2h or 1h30m.
	PartyDeadlinePattern = "^(?:[0-9]+h)?(?:[0-9]+m)?$"
)
