// Package slack talks to the Slack Web API for the PR notifier: posting,
// updating, and deleting messages, adding emoji reactions, and composing the
// text of the notification.
package slack

import (
	"fmt"
	"strings"
)

// PRDetails is the subset of PR information the Composer needs to render a
// notification. It is detached from any HTTP payload type so the composer
// stays a pure function of its inputs.
type PRDetails struct {
	Repository string
	Number     int
	Title      string
	URL        string
	Author     string
}

// Composer renders Slack-formatted notification messages.
type Composer struct {
	newPREmoji string
}

// NewComposer returns a Composer that prefixes new-PR messages with the given
// reaction-style emoji name (without colons).
func NewComposer(newPREmoji string) *Composer {
	return &Composer{newPREmoji: newPREmoji}
}

// NewMessage renders the initial Slack message for a PR. Mentions are joined
// with commas (matching the legacy PHP composer's wire format). When the
// mentions list is empty, the prefix is omitted entirely so the message has
// no stranded ", ".
func (c *Composer) NewMessage(pr PRDetails, mentions []string) string {
	return fmt.Sprintf(
		":%s: %splease review <%s|PR #%d: %s> by %s",
		c.newPREmoji,
		mentionsPrefix(mentions),
		pr.URL,
		pr.Number,
		pr.Title,
		pr.Author,
	)
}

// BotMessage renders the compact notification for a PR opened by a dependency
// bot. bot is the lowercase bot name ("dependabot" / "renovate"). When
// security is true it uses the rotating-light advisory template; otherwise the
// package routine-bump template. Mentions follow the same empty-list rule as
// NewMessage. The PR author is intentionally omitted — the bot name carries it.
func (c *Composer) BotMessage(pr PRDetails, mentions []string, bot string, security bool) string {
	emoji, verb := "package", "bumped"
	if security {
		emoji, verb = "rotating_light", "security update"
	}
	return fmt.Sprintf(
		":%s: %s%s %s <%s|PR #%d: %s>",
		emoji,
		mentionsPrefix(mentions),
		bot,
		verb,
		pr.URL,
		pr.Number,
		pr.Title,
	)
}

// mentionsPrefix joins mentions with commas (the legacy PHP wire format) and
// appends ", "; an empty list yields "" so the message has no stranded ", ".
func mentionsPrefix(mentions []string) string {
	if len(mentions) == 0 {
		return ""
	}
	return strings.Join(mentions, ",") + ", "
}

// UpdatedMessage wraps the previous message in the closed-PR decoration:
// "[Merged] ~<original>~" or "[Closed] ~<original>~".
func (c *Composer) UpdatedMessage(merged bool, original string) string {
	state := "Closed"
	if merged {
		state = "Merged"
	}
	return fmt.Sprintf("[%s] ~%s~", state, original)
}
