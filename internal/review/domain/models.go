package domain

import (
	"encoding/json"
	"log/slog"
	"time"
)

// StartReviewCommand is the parsed intent of a verified "Start review" button
// click: who clicked, on which PR, and the message to decorate.
type StartReviewCommand struct {
	Repository string
	PRNumber   int
	Reviewer   Reviewer
	Message    MessageRef
}

// Reviewer is the Slack user who clicked "Start review". UserID is the stable
// "U…" identifier; UserName is a display convenience and may be absent.
type Reviewer struct {
	UserID   string
	UserName string
}

// MessageRef addresses the interactive PR message and carries its original block
// array (echoed back by Slack) so the decorator can append a marker without
// re-composing the whole message. RawBlocks is opaque JSON passed through
// untouched; Fallback is the message's top-level plain-text.
type MessageRef struct {
	Channel   string
	TS        string
	RawBlocks json.RawMessage
	Fallback  string
}

// HandlerParams bundles the start-review use case's dependencies. Now supplies
// the clock (time.Now in production, a fixed clock in tests).
type HandlerParams struct {
	Recorder  Recorder
	Messages  MessageChecker
	Decorator MessageDecorator
	Logger    *slog.Logger
	Now       func() time.Time
}
