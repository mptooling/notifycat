package domain

// ChannelMention is the Slack wire token used when an entry omits the
// `mentions:` key — operators see `@channel` in Slack.
const ChannelMention = "<!channel>"

// WildcardKey is the org-level tier key (the literal "*"): it both supplies
// defaults that explicit repo tiers inherit and matches any repo not named
// explicitly.
const WildcardKey = "*"

// NotificationStrategy decides when a PR event reaches Slack.
type NotificationStrategy string

const (
	// StrategyPerEvent posts or updates one Slack message per PR as each event
	// arrives. It is the default when no tier sets a strategy.
	StrategyPerEvent NotificationStrategy = "per-event"
	// StrategyScheduled queues new PRs and posts one list message per channel
	// on the tier's schedule.
	StrategyScheduled NotificationStrategy = "scheduled"
)

// ScheduleMode is how a scheduled strategy writes its send times.
type ScheduleMode string

const (
	// ScheduleModeHourly sends at the top of every hour.
	ScheduleModeHourly ScheduleMode = "hourly"
	// ScheduleModeAt sends once a day at each listed time.
	ScheduleModeAt ScheduleMode = "at"
	// ScheduleModeCron sends on each listed cron spec.
	ScheduleModeCron ScheduleMode = "cron"
)
