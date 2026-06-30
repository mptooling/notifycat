package slackhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
)

// Interaction is the parsed view of a Slack interaction envelope, holding only
// the fields notifycat uses. Slack sends a much larger object; unknown fields
// are ignored. Extending behavior usually means adding a field here rather than
// writing a new parser.
type Interaction struct {
	Type        string
	User        User
	Channel     Channel
	Message     Message
	Actions     []Action
	ResponseURL string
	TriggerID   string
}

// User is the Slack user who triggered the interaction. ID is the stable
// "U…" identifier; Username is a display convenience and may be absent.
type User struct {
	ID       string
	Username string
}

// Channel is the conversation the interactive message lives in.
type Channel struct {
	ID string
}

// Message identifies the message that carried the interactive component, by its
// Slack timestamp ("ts").
type Message struct {
	TS string
}

// Action is a single interactive element the user activated. ActionID is the
// element's configured identifier (e.g. "start_review"); Value is its opaque
// payload (e.g. an encoded repo + PR number).
type Action struct {
	ActionID string
	Value    string
}

// ErrMissingPayload is returned when the form body has no `payload` field.
var ErrMissingPayload = errors.New("slackhook: missing payload field")

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
		TS string `json:"ts"`
	} `json:"message"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
	ResponseURL string `json:"response_url"`
	TriggerID   string `json:"trigger_id"`
}

// ParseInteraction decodes a Slack interaction request body. The body is
// application/x-www-form-urlencoded with a single `payload` field holding
// URL-encoded JSON.
func ParseInteraction(body []byte) (Interaction, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return Interaction{}, fmt.Errorf("slackhook: parse form: %w", err)
	}
	encoded := values.Get("payload")
	if encoded == "" {
		return Interaction{}, ErrMissingPayload
	}

	var raw rawInteraction
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		return Interaction{}, fmt.Errorf("slackhook: decode payload: %w", err)
	}

	interaction := Interaction{
		Type:        raw.Type,
		User:        User{ID: raw.User.ID, Username: raw.User.Username},
		Channel:     Channel{ID: raw.Channel.ID},
		Message:     Message{TS: raw.Message.TS},
		ResponseURL: raw.ResponseURL,
		TriggerID:   raw.TriggerID,
	}
	for _, a := range raw.Actions {
		interaction.Actions = append(interaction.Actions, Action{ActionID: a.ActionID, Value: a.Value})
	}
	return interaction, nil
}
