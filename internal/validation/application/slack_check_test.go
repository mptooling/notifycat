package application_test

import (
	"context"
	"strings"
	"testing"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/application"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

func TestValidate_InvalidChannelFormat_ShortCircuitsSlackProbe(t *testing.T) {
	m, s, gh := happy()
	m.get = func(_ context.Context, _ string) (routingdomain.RepoMapping, error) {
		return routingdomain.RepoMapping{Repository: "acme/widgets", SlackChannel: "not-a-channel"}, nil
	}
	s.conversationsInfo = func(_ context.Context, _ string) (domain.ChannelInfo, error) {
		t.Fatal("ConversationsInfo should not be called when channel format is invalid")
		return domain.ChannelInfo{}, nil
	}
	v := application.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	if c := findCheck(t, r, "channel-format"); c.Status != domain.StatusFail {
		t.Fatalf("channel-format = %+v", c)
	}
	if c := findCheck(t, r, "slack-channel"); c.Status != domain.StatusSkip {
		t.Fatalf("slack-channel should be skipped, got %+v", c)
	}
}

func TestValidate_InvalidAuthToken(t *testing.T) {
	m, s, gh := happy()
	s.authTest = func(_ context.Context) (string, []string, error) {
		return "", nil, &domain.SlackAPIError{Method: "auth.test", Code: "invalid_auth"}
	}
	v := application.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-auth")
	if c.Status != domain.StatusFail || !strings.Contains(c.Detail, "invalid or revoked") {
		t.Fatalf("slack-auth = %+v", c)
	}
}

func TestValidate_MissingScope(t *testing.T) {
	m, s, gh := happy()
	s.authTest = func(_ context.Context) (string, []string, error) {
		return "UBOT", []string{"chat:write"}, nil // reactions:write missing
	}
	v := application.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-auth")
	if c.Status != domain.StatusFail || !strings.Contains(c.Detail, "reactions:write") {
		t.Fatalf("slack-auth = %+v", c)
	}
}

func TestValidate_ChannelNotFound(t *testing.T) {
	m, s, gh := happy()
	s.conversationsInfo = func(_ context.Context, _ string) (domain.ChannelInfo, error) {
		return domain.ChannelInfo{}, &domain.SlackAPIError{Method: "conversations.info", Code: "channel_not_found"}
	}
	v := application.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-channel")
	if c.Status != domain.StatusFail || !strings.Contains(c.Detail, "does not exist") {
		t.Fatalf("slack-channel = %+v", c)
	}
}

func TestValidate_BotNotMember(t *testing.T) {
	m, s, gh := happy()
	s.conversationsInfo = func(_ context.Context, _ string) (domain.ChannelInfo, error) {
		return domain.ChannelInfo{ID: "C1234567", Name: "general", IsMember: false}, nil
	}
	v := application.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-channel")
	if c.Status != domain.StatusFail || !strings.Contains(c.Detail, "not a member") {
		t.Fatalf("slack-channel = %+v", c)
	}
}

func TestValidate_ChannelArchived(t *testing.T) {
	m, s, gh := happy()
	s.conversationsInfo = func(_ context.Context, _ string) (domain.ChannelInfo, error) {
		return domain.ChannelInfo{ID: "C1234567", Name: "old", IsMember: true, IsArchived: true}, nil
	}
	v := application.NewValidator(m, s, gh)

	r := v.Validate(context.Background(), "acme/widgets")
	c := findCheck(t, r, "slack-channel")
	if c.Status != domain.StatusFail || !strings.Contains(c.Detail, "archived") {
		t.Fatalf("slack-channel = %+v", c)
	}
}
