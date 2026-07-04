package infrastructure

import (
	"context"
	"errors"

	"github.com/mptooling/notifycat/internal/platform/slack"
	"github.com/mptooling/notifycat/internal/validation/domain"
)

// SlackProbe adapts the platform Slack client to the validation domain's
// SlackChecker port. It maps the client's ChannelInfo to the domain type and
// translates the client's API error into the domain's SlackAPIError so the
// application can classify failures without importing the Slack SDK.
type SlackProbe struct {
	client *slack.Client
}

var _ domain.SlackChecker = (*SlackProbe)(nil)

// NewSlackProbe wraps a Slack client as a validation SlackChecker.
func NewSlackProbe(client *slack.Client) *SlackProbe {
	return &SlackProbe{client: client}
}

// AuthTest verifies the token and returns the granted scopes, translating any
// Slack API error into a domain SlackAPIError.
func (p *SlackProbe) AuthTest(ctx context.Context) (userID string, scopes []string, err error) {
	userID, scopes, err = p.client.AuthTest(ctx)
	return userID, scopes, translateSlackError(err)
}

// ConversationsInfo returns the domain view of a channel's metadata,
// translating any Slack API error into a domain SlackAPIError.
func (p *SlackProbe) ConversationsInfo(ctx context.Context, channel string) (domain.ChannelInfo, error) {
	info, err := p.client.ConversationsInfo(ctx, channel)
	if err != nil {
		return domain.ChannelInfo{}, translateSlackError(err)
	}
	return domain.ChannelInfo{
		ID:         info.ID,
		Name:       info.Name,
		IsMember:   info.IsMember,
		IsArchived: info.IsArchived,
	}, nil
}

// translateSlackError maps a *slack.APIError to a *domain.SlackAPIError so the
// application layer can classify Slack failures without importing the SDK.
// Non-API (transport) errors and nil pass through unchanged.
func translateSlackError(err error) error {
	var apiErr *slack.APIError
	if errors.As(err, &apiErr) {
		return &domain.SlackAPIError{Method: apiErr.Method, Code: apiErr.Code}
	}
	return err
}
