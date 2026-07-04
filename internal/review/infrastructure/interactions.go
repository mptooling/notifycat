package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mptooling/notifycat/internal/platform/httpx"
	"github.com/mptooling/notifycat/internal/platform/security"
	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
)

// MaxBodyBytes caps the size of an accepted interaction body. Slack's interaction
// payloads are small, so 1 MiB is generous and guards against memory exhaustion.
const MaxBodyBytes int64 = 1 << 20 // 1 MiB

const startReviewActionID = "start_review"

// SignatureMiddleware returns an HTTP middleware that rejects oversized bodies
// (413) and requests with a missing/invalid/stale Slack signature (401), passing
// a fresh body reader to next. The signature is verified over the raw bytes
// before any form parsing.
func SignatureMiddleware(verifier security.SignatureVerifier) func(http.Handler) http.Handler {
	return httpx.Signature(MaxBodyBytes, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		signature := r.Header.Get(security.SlackSignatureHeader)
		timestamp := r.Header.Get(security.SlackTimestampHeader)
		if signature == "" || timestamp == "" {
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return false
		}
		// Build the compound "<timestamp>\n<v0=hex>" string security.SlackVerifier
		// expects, which must satisfy the two-argument SignatureVerifier interface
		// while still covering the timestamp.
		compound := timestamp + "\n" + signature
		if err := verifier.Verify(body, compound); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return false
		}
		return true
	})
}

// Interaction is the parsed view of a Slack interaction envelope, holding only
// the fields notifycat uses.
type Interaction struct {
	Type        string
	User        User
	Channel     Channel
	Message     Message
	Actions     []Action
	ResponseURL string
	TriggerID   string
}

// User is the Slack user who triggered the interaction.
type User struct {
	ID       string
	Username string
}

// Channel is the conversation the interactive message lives in.
type Channel struct {
	ID string
}

// Message identifies the message that carried the interactive component.
// RawBlocks is the original blocks array echoed back by Slack for passthrough.
type Message struct {
	TS        string
	Text      string
	RawBlocks json.RawMessage
}

// Action is a single interactive element the user activated.
type Action struct {
	ActionID string
	Value    string
}

// ErrMissingPayload is returned when the form body has no `payload` field.
var ErrMissingPayload = errors.New("review: missing payload field")

// rawInteraction mirrors only the JSON fields we read.
type rawInteraction struct {
	Type string `json:"type"`
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Message struct {
		TS     string          `json:"ts"`
		Text   string          `json:"text"`
		Blocks json.RawMessage `json:"blocks"`
	} `json:"message"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
	ResponseURL string `json:"response_url"`
	TriggerID   string `json:"trigger_id"`
}

// ParseInteraction decodes a Slack interaction request body (an
// application/x-www-form-urlencoded body with a single `payload` field holding
// URL-encoded JSON).
func ParseInteraction(body []byte) (Interaction, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return Interaction{}, fmt.Errorf("review: parse form: %w", err)
	}
	encoded := values.Get("payload")
	if encoded == "" {
		return Interaction{}, ErrMissingPayload
	}

	var raw rawInteraction
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		return Interaction{}, fmt.Errorf("review: decode payload: %w", err)
	}

	interaction := Interaction{
		Type:        raw.Type,
		User:        User{ID: raw.User.ID, Username: raw.User.Username},
		Channel:     Channel{ID: raw.Channel.ID},
		Message:     Message{TS: raw.Message.TS, Text: raw.Message.Text, RawBlocks: raw.Message.Blocks},
		ResponseURL: raw.ResponseURL,
		TriggerID:   raw.TriggerID,
	}
	for _, a := range raw.Actions {
		interaction.Actions = append(interaction.Actions, Action{ActionID: a.ActionID, Value: a.Value})
	}
	return interaction, nil
}

// InteractionSink receives a parsed Interaction.
type InteractionSink func(ctx context.Context, interaction Interaction) error

// NewInteractionsHandler returns an http.Handler that parses a Slack interaction
// body and forwards it to sink. It assumes the body was verified by
// SignatureMiddleware. Slack retries any non-2xx within ~3s, so the handler
// always responds 200 once the signature is trusted: a malformed or unactionable
// payload is logged and ignored. A sink error is logged but does not change the
// response.
func NewInteractionsHandler(sink InteractionSink, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("slack interaction: read body", slog.Any("err", err))
			w.WriteHeader(http.StatusOK)
			return
		}
		interaction, err := ParseInteraction(body)
		if err != nil {
			logger.Warn("ignored slack interaction", slog.String("reason", "unparseable_payload"), slog.Any("err", err))
			w.WriteHeader(http.StatusOK)
			return
		}

		logger.Info("slack interaction received",
			slog.String("type", interaction.Type),
			slog.String("action_id", firstActionID(interaction)),
			slog.String("user", interaction.User.ID),
		)

		if sink != nil {
			if err := sink(r.Context(), interaction); err != nil {
				logger.Error("slack interaction: sink failed",
					slog.String("type", interaction.Type),
					slog.Any("err", err))
			}
		}
		w.WriteHeader(http.StatusOK)
	})
}

// firstActionID returns the action_id of the first action, or "" when none.
func firstActionID(interaction Interaction) string {
	if len(interaction.Actions) == 0 {
		return ""
	}
	return interaction.Actions[0].ActionID
}

// NewStartReviewSink adapts the review StartReview use case to an InteractionSink:
// it maps a "start_review" block_actions click to a StartReviewCommand and
// invokes the use case. Non-actionable interactions (wrong type, wrong action,
// malformed value) are ignored.
func NewStartReviewSink(handler reviewdomain.StartReview, logger *slog.Logger) InteractionSink {
	return func(ctx context.Context, interaction Interaction) error {
		if interaction.Type != "block_actions" {
			return nil
		}
		action, ok := firstAction(interaction)
		if !ok || action.ActionID != startReviewActionID {
			return nil
		}
		repository, prNumber, ok := decodeValue(action.Value)
		if !ok {
			logger.Debug("ignored start_review",
				slog.String("reason", "malformed_value"), slog.String("value", action.Value))
			return nil
		}
		return handler.Handle(ctx, reviewdomain.StartReviewCommand{
			Repository: repository,
			PRNumber:   prNumber,
			Reviewer:   reviewdomain.Reviewer{UserID: interaction.User.ID, UserName: interaction.User.Username},
			Message: reviewdomain.MessageRef{
				Channel:   interaction.Channel.ID,
				TS:        interaction.Message.TS,
				RawBlocks: interaction.Message.RawBlocks,
				Fallback:  interaction.Message.Text,
			},
		})
	}
}

func firstAction(interaction Interaction) (Action, bool) {
	if len(interaction.Actions) == 0 {
		return Action{}, false
	}
	return interaction.Actions[0], true
}

// decodeValue parses the button value "repository#number" (e.g. "octo/web#42").
// A GitHub repository name cannot contain '#', so the last '#' splits repo from
// PR number unambiguously.
func decodeValue(value string) (string, int, bool) {
	hashIndex := strings.LastIndex(value, "#")
	if hashIndex <= 0 || hashIndex == len(value)-1 {
		return "", 0, false
	}
	repository := value[:hashIndex]
	prNumber, err := strconv.Atoi(value[hashIndex+1:])
	if err != nil || prNumber <= 0 {
		return "", 0, false
	}
	return repository, prNumber, true
}
