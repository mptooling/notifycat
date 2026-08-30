package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/validation/application"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

func TestValidate_InvalidChannelFormat_ShortCircuitsSlackProbe(t *testing.T) {
	mappings, slack, hooks := happy()
	mappings.get = func(context.Context, string) (routingdomain.RepoMapping, error) {
		return routingdomain.RepoMapping{Repository: "acme/widgets", SlackChannel: "not-a-channel"}, nil
	}
	slack.conversationsInfo = func(context.Context, string) (domain.ChannelInfo, error) {
		assert.Fail(t, "a malformed channel must not reach Slack")
		return domain.ChannelInfo{}, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assert.Equal(t, domain.StatusFail, findCheck(t, report, "channel-format").Status)
	assert.Equal(t, domain.StatusSkip, findCheck(t, report, "slack-channel").Status)
}

func TestValidate_InvalidAuthToken(t *testing.T) {
	mappings, slack, hooks := happy()
	slack.authTest = func(context.Context) (string, []string, error) {
		return "", nil, &domain.SlackAPIError{Method: "auth.test", Code: "invalid_auth"}
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assertCheckFails(t, report, "slack-auth", "invalid or revoked")
}

func TestValidate_MissingScope(t *testing.T) {
	mappings, slack, hooks := happy()
	slack.authTest = func(context.Context) (string, []string, error) {
		return "UBOT", []string{"chat:write"}, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assertCheckFails(t, report, "slack-auth", "reactions:write")
}

func TestValidate_ChannelNotFound(t *testing.T) {
	mappings, slack, hooks := happy()
	slack.conversationsInfo = func(context.Context, string) (domain.ChannelInfo, error) {
		return domain.ChannelInfo{}, &domain.SlackAPIError{Method: "conversations.info", Code: "channel_not_found"}
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assertCheckFails(t, report, "slack-channel", "does not exist")
}

func TestValidate_BotNotMember(t *testing.T) {
	mappings, slack, hooks := happy()
	slack.conversationsInfo = func(context.Context, string) (domain.ChannelInfo, error) {
		return domain.ChannelInfo{ID: "C1234567", Name: "general", IsMember: false}, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assertCheckFails(t, report, "slack-channel", "not a member")
}

func TestValidate_ChannelArchived(t *testing.T) {
	mappings, slack, hooks := happy()
	slack.conversationsInfo = func(context.Context, string) (domain.ChannelInfo, error) {
		return domain.ChannelInfo{ID: "C1234567", Name: "old", IsMember: true, IsArchived: true}, nil
	}
	validator := application.NewValidator(mappings, slack, githubProbe(hooks))

	report := validator.Validate(context.Background(), "acme/widgets")

	assertCheckFails(t, report, "slack-channel", "archived")
}
