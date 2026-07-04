package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/slack"
)

// SlackPoster adapts the platform Slack client to the digest's DigestPoster
// port, mapping the domain's neutral Message back to the Slack message type at
// the boundary.
type SlackPoster struct {
	client *slack.Client
}

// NewSlackPoster wraps the platform Slack client.
func NewSlackPoster(client *slack.Client) *SlackPoster {
	return &SlackPoster{client: client}
}

// PostMessage implements domain.DigestPoster.
func (p *SlackPoster) PostMessage(ctx context.Context, channel string, msg domain.Message) (string, error) {
	return p.client.PostMessage(ctx, channel, toSlackMessage(msg))
}

// PostReply implements domain.DigestPoster.
func (p *SlackPoster) PostReply(ctx context.Context, channel, threadTS string, msg domain.Message) (string, error) {
	return p.client.PostReply(ctx, channel, threadTS, toSlackMessage(msg))
}

// toSlackMessage maps the domain's neutral Message to the platform Slack message
// type. The digest emits only section blocks (Text set), so Elements and Buttons
// are always empty here.
func toSlackMessage(m domain.Message) slack.Message {
	blocks := make([]slack.Block, len(m.Blocks))
	for i, block := range m.Blocks {
		var text *slack.TextObject
		if block.Text != nil {
			text = &slack.TextObject{Type: block.Text.Type, Text: block.Text.Text}
		}
		blocks[i] = slack.Block{Type: block.Type, Text: text}
	}
	return slack.Message{Blocks: blocks, Fallback: m.Fallback}
}

var _ domain.DigestPoster = (*SlackPoster)(nil)
