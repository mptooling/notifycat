package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/slack"
)

// SlackSmokeReactions implements diagnosticsdomain.SmokeReactions over
// *slack.Client, returning only the emoji name strings.
type SlackSmokeReactions struct {
	client *slack.Client
}

// NewSlackSmokeReactions returns a SmokeReactions backed by the given Slack client.
func NewSlackSmokeReactions(client *slack.Client) *SlackSmokeReactions {
	return &SlackSmokeReactions{client: client}
}

// Reactions calls reactions.get and returns only the emoji names from each
// Reaction entry.
func (s *SlackSmokeReactions) Reactions(ctx context.Context, channel, ts string) ([]string, error) {
	slackReactions, err := s.client.GetReactions(ctx, channel, ts)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(slackReactions))
	for i, r := range slackReactions {
		names[i] = r.Name
	}
	return names, nil
}
