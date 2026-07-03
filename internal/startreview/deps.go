// Package startreview turns a verified "Start review" button click into a
// recorded review plus an in-place update of the PR's Slack message.
package startreview

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// Reviews records and inspects per-PR review sessions.
type Reviews interface {
	ActiveForUser(ctx context.Context, repository string, prNumber int, slackUserID string) (store.CodeReview, error)
	Start(ctx context.Context, repository string, prNumber int, slackUserID, slackUserName string) error
}

// Messages confirms notifycat owns a message for the PR before acting on a click.
type Messages interface {
	Messages(ctx context.Context, repository string, prNumber int) ([]store.Message, error)
}

// SlackUpdater edits the PR message in place, passing the original blocks
// through untouched with the reviewer marker appended.
type SlackUpdater interface {
	UpdateMessageRawBlocks(ctx context.Context, channel, ts string, blocks []json.RawMessage, fallback string) error
}

// Composer renders the reviewer marker block.
type Composer interface {
	ReviewingMarker(userID string, since time.Time) slack.Block
}
