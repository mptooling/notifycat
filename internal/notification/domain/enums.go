package domain

// Ignored-event reason codes, logged on every silent no-op so a 200-OK delivery
// that changed nothing can be triaged.
const (
	ReasonNoHandler       = "no_handler"
	ReasonNoMapping       = "no_mapping"
	ReasonNoStoredMessage = "no_stored_message"
	ReasonAlreadySent     = "already_sent"
)

// BotKind classifies a PR author as a known dependency bot (or none). Moved from
// the botpr package; drives the compact dependency-bot open template.
type BotKind int

// Recognised dependency-bot kinds.
const (
	BotKindNone BotKind = iota
	BotKindDependabot
	BotKindRenovate
)

// Name returns the bot's display name ("dependabot"/"renovate"), or "" for none.
func (k BotKind) Name() string {
	switch k {
	case BotKindDependabot:
		return "dependabot"
	case BotKindRenovate:
		return "renovate"
	default:
		return ""
	}
}
