package validate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
)

func TestValidate_InvalidChannelFormat_ShortCircuitsSlackProbe(t *testing.T) {
	m, s, gh := happy()
	m.get = func(_ context.Context, _ string) (store.RepoMapping, error) {
		return store.RepoMapping{Repository: "acme/widgets", SlackChannel: "not-a-channel"}, nil
	}
	s.conversationsInfo = func(_ context.Context, _ string) (slack.ChannelInfo, error) {
		t.Fatal("ConversationsInfo should not be called when channel format is invalid")
		return slack.ChannelInfo{}, nil
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	if c := findCheck(t, r, "channel-format"); c.Status != validate.StatusFail {
		t.Fatalf("channel-format = %+v", c)
	}
	if c := findCheck(t, r, "slack-channel"); c.Status != validate.StatusSkip {
		t.Fatalf("slack-channel should be skipped, got %+v", c)
	}
}

func TestValidate_InvalidAuthToken(t *testing.T) {
	m, s, gh := happy()
	s.authTest = func(_ context.Context) (string, []string, error) {
		return "", nil, &slack.APIError{Method: "auth.test", Code: "invalid_auth"}
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-auth")
	if c.Status != validate.StatusFail || !strings.Contains(c.Detail, "invalid or revoked") {
		t.Fatalf("slack-auth = %+v", c)
	}
}

func TestValidate_MissingScope(t *testing.T) {
	m, s, gh := happy()
	s.authTest = func(_ context.Context) (string, []string, error) {
		return "UBOT", []string{"chat:write"}, nil // reactions:write missing
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-auth")
	if c.Status != validate.StatusFail || !strings.Contains(c.Detail, "reactions:write") {
		t.Fatalf("slack-auth = %+v", c)
	}
}

func TestValidate_ChannelNotFound(t *testing.T) {
	m, s, gh := happy()
	s.conversationsInfo = func(_ context.Context, _ string) (slack.ChannelInfo, error) {
		return slack.ChannelInfo{}, &slack.APIError{Method: "conversations.info", Code: "channel_not_found"}
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-channel")
	if c.Status != validate.StatusFail || !strings.Contains(c.Detail, "does not exist") {
		t.Fatalf("slack-channel = %+v", c)
	}
}

func TestValidate_BotNotMember(t *testing.T) {
	m, s, gh := happy()
	s.conversationsInfo = func(_ context.Context, _ string) (slack.ChannelInfo, error) {
		return slack.ChannelInfo{ID: "C1234567", Name: "general", IsMember: false}, nil
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-channel")
	if c.Status != validate.StatusFail || !strings.Contains(c.Detail, "not a member") {
		t.Fatalf("slack-channel = %+v", c)
	}
}

func TestValidate_ChannelArchived(t *testing.T) {
	m, s, gh := happy()
	s.conversationsInfo = func(_ context.Context, _ string) (slack.ChannelInfo, error) {
		return slack.ChannelInfo{ID: "C1234567", Name: "old", IsMember: true, IsArchived: true}, nil
	}
	v := validate.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-channel")
	if c.Status != validate.StatusFail || !strings.Contains(c.Detail, "archived") {
		t.Fatalf("slack-channel = %+v", c)
	}
}
