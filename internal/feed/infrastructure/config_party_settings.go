package infrastructure

import (
	"context"

	"github.com/mptooling/notifycat/internal/feed/domain"
)

// ConfigPartySettings resolves party settings from the global config.
type ConfigPartySettings struct {
	settings domain.Settings
}

var _ domain.PartySettingsResolver = (*ConfigPartySettings)(nil)

// NewConfigPartySettings builds a resolver that returns the global party settings for every pull request.
func NewConfigPartySettings(settings domain.Settings) *ConfigPartySettings {
	return &ConfigPartySettings{settings: settings}
}

// Resolve implements domain.PartySettingsResolver.
func (resolver *ConfigPartySettings) Resolve(context.Context, domain.PullRequestKey) (domain.PartySettings, error) {
	return resolver.settings.Party, nil
}
