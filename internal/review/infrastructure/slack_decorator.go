package infrastructure

import (
	"context"
	"encoding/json"
	"time"

	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
	"github.com/mptooling/notifycat/internal/slack"
)

// SlackDecorator implements the review MessageDecorator port over the Slack
// composer and client. It appends a "reviewing" marker to the PR message,
// passing the original blocks through untouched.
type SlackDecorator struct {
	composer *slack.Composer
	client   *slack.Client
}

// NewSlackDecorator wraps the Slack composer and client.
func NewSlackDecorator(composer *slack.Composer, client *slack.Client) *SlackDecorator {
	return &SlackDecorator{composer: composer, client: client}
}

// AppendReviewingMarker composes the reviewing marker for the reviewer, inserts
// it immediately before the message's actions (button) block, and updates the
// message in place.
func (d *SlackDecorator) AppendReviewingMarker(ctx context.Context, message reviewdomain.MessageRef, reviewer reviewdomain.Reviewer, since time.Time) error {
	marker := d.composer.ReviewingMarker(reviewer.UserID, since)
	rawMarker, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	blocks := insertBeforeActions(splitBlocks(message.RawBlocks), rawMarker)
	return d.client.UpdateMessageRawBlocks(ctx, message.Channel, message.TS, blocks, message.Fallback)
}

// splitBlocks decodes a raw Slack blocks array into its element raws. A missing
// or malformed array yields nil, so the update still posts the marker alone.
func splitBlocks(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	return blocks
}

// insertBeforeActions places marker immediately before the first "actions" block
// (the button row) so the reviewer line renders above the button; with no
// actions block it appends.
func insertBeforeActions(blocks []json.RawMessage, marker json.RawMessage) []json.RawMessage {
	for i, block := range blocks {
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(block, &probe); err == nil && probe.Type == "actions" {
			out := make([]json.RawMessage, 0, len(blocks)+1)
			out = append(out, blocks[:i]...)
			out = append(out, marker)
			out = append(out, blocks[i:]...)
			return out
		}
	}
	return append(blocks, marker)
}

var _ reviewdomain.MessageDecorator = (*SlackDecorator)(nil)
