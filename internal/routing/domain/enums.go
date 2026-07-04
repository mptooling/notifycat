package domain

// ChannelMention is the Slack wire token used when an entry omits the
// `mentions:` key — operators see `@channel` in Slack.
const ChannelMention = "<!channel>"

// WildcardKey is the org-level tier key (the literal "*"): it both supplies
// defaults that explicit repo tiers inherit and matches any repo not named
// explicitly.
const WildcardKey = "*"
