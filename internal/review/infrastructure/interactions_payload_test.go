package infrastructure

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// formEncode wraps a JSON interaction the way Slack does: a single,
// URL-encoded `payload` field in an application/x-www-form-urlencoded body.
func formEncode(jsonPayload string) []byte {
	return []byte("payload=" + url.QueryEscape(jsonPayload))
}

func TestParseInteraction_BlockActions(t *testing.T) {
	body := formEncode(`{
		"type": "block_actions",
		"user": {"id": "U123", "username": "alice"},
		"channel": {"id": "C999"},
		"message": {"ts": "1700000000.000100"},
		"response_url": "https://hooks.slack.com/actions/abc",
		"trigger_id": "trig-1",
		"actions": [{"action_id": "start_review", "value": "octo/widget#42"}]
	}`)

	interaction, err := ParseInteraction(body)
	if err != nil {
		t.Fatalf("ParseInteraction: %v", err)
	}
	if interaction.Type != "block_actions" {
		t.Errorf("Type = %q; want block_actions", interaction.Type)
	}
	if interaction.User.ID != "U123" || interaction.User.Username != "alice" {
		t.Errorf("User = %+v", interaction.User)
	}
	if interaction.Channel.ID != "C999" {
		t.Errorf("Channel.ID = %q; want C999", interaction.Channel.ID)
	}
	if interaction.Message.TS != "1700000000.000100" {
		t.Errorf("Message.TS = %q", interaction.Message.TS)
	}
	if interaction.ResponseURL != "https://hooks.slack.com/actions/abc" {
		t.Errorf("ResponseURL = %q", interaction.ResponseURL)
	}
	if interaction.TriggerID != "trig-1" {
		t.Errorf("TriggerID = %q", interaction.TriggerID)
	}
	if len(interaction.Actions) != 1 {
		t.Fatalf("Actions = %+v; want 1", interaction.Actions)
	}
	if interaction.Actions[0].ActionID != "start_review" || interaction.Actions[0].Value != "octo/widget#42" {
		t.Errorf("Action[0] = %+v", interaction.Actions[0])
	}
}

func TestParseInteraction_MissingPayloadField(t *testing.T) {
	_, err := ParseInteraction([]byte("other=1"))
	if err == nil {
		t.Fatal("ParseInteraction(no payload field) = nil; want error")
	}
	if !errors.Is(err, ErrMissingPayload) {
		t.Errorf("error = %v; want ErrMissingPayload", err)
	}
}

func TestParseInteraction_MalformedJSON(t *testing.T) {
	if _, err := ParseInteraction(formEncode("not-json")); err == nil {
		t.Fatal("ParseInteraction(malformed JSON) = nil; want error")
	}
}

func TestParseInteraction_CapturesMessageBlocksAndText(t *testing.T) {
	payload := `{"type":"block_actions","user":{"id":"U1","username":"ada"},"channel":{"id":"C1"},"message":{"ts":"1.1","text":"please review","blocks":[{"type":"section"}]},"actions":[{"action_id":"start_review","value":"octo/web#42"}]}`
	body := "payload=" + url.QueryEscape(payload)

	got, err := ParseInteraction([]byte(body))
	if err != nil {
		t.Fatalf("ParseInteraction: %v", err)
	}
	if got.Message.Text != "please review" {
		t.Errorf("Text = %q", got.Message.Text)
	}
	if !strings.Contains(string(got.Message.RawBlocks), `"section"`) {
		t.Errorf("RawBlocks = %s", got.Message.RawBlocks)
	}
	if got.Actions[0].Value != "octo/web#42" {
		t.Errorf("Value = %q", got.Actions[0].Value)
	}
}

func TestParseInteraction_NoActions(t *testing.T) {
	// An interaction type we don't act on (e.g. a shortcut) still parses; the
	// handler decides what to ignore.
	interaction, err := ParseInteraction(formEncode(`{"type": "shortcut"}`))
	if err != nil {
		t.Fatalf("ParseInteraction: %v", err)
	}
	if interaction.Type != "shortcut" {
		t.Errorf("Type = %q; want shortcut", interaction.Type)
	}
	if len(interaction.Actions) != 0 {
		t.Errorf("Actions = %+v; want empty", interaction.Actions)
	}
}
