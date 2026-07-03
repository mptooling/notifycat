package domain

// DefaultDigestSchedule is the cron spec used when the digest section is absent
// or omits `schedule`: 9am every morning, in the configured digest timezone
// (default UTC; see DigestConfig.Timezone).
const DefaultDigestSchedule = "0 9 * * *"
