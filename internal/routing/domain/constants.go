package domain

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
