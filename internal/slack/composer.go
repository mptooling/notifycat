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
// with commas (matching the legacy PHP composer's wire format).
func (c *Composer) NewMessage(pr PRDetails, mentions []string) string {
	return fmt.Sprintf(
		":%s: %s, please review <%s|PR #%d: %s> by %s",
		c.newPREmoji,
		strings.Join(mentions, ","),
		pr.URL,
		pr.Number,
		pr.Title,
		pr.Author,
	)
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
