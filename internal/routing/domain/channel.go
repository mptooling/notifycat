package domain

import "regexp"

// channelPattern is a Slack channel id: a public (C), private (G) or DM (D)
// room. Mappings carry ids, never names, so a stored channel and a configured
// one are always directly comparable.
var channelPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{2,}$`)

// IsChannelID reports whether value is a well-formed Slack channel id.
func IsChannelID(value string) bool {
	return channelPattern.MatchString(value)
}
