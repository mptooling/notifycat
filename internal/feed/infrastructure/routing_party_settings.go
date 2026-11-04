package infrastructure

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mptooling/notifycat/internal/feed/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// RoutingPartySettings resolves party settings from the routing rules and the changed files of a pull request.
type RoutingPartySettings struct {
	rules  routingdomain.PartyRules
	files  routingdomain.ChangedFilesReader
	logger *slog.Logger
}

var _ domain.PartySettingsResolver = (*RoutingPartySettings)(nil)

// NewRoutingPartySettings builds the resolver. A nil files reader means no read token is configured.
func NewRoutingPartySettings(rules routingdomain.PartyRules, files routingdomain.ChangedFilesReader, logger *slog.Logger) *RoutingPartySettings {
	return &RoutingPartySettings{rules: rules, files: files, logger: logger}
}

// Resolve implements domain.PartySettingsResolver.
func (resolver *RoutingPartySettings) Resolve(ctx context.Context, key domain.PullRequestKey) domain.PartyResolution {
	if resolver.files == nil || !resolver.rules.HasPartyPathRules(key.Repository) {
		return domain.PartyResolution{Settings: toFeedPartySettings(resolver.rules.PartySettings(key.Repository, nil))}
	}
	changedFiles, err := resolver.readChangedFiles(ctx, key)
	if err != nil {
		resolver.logger.Warn("feed party settings fallback", "repository", key.Repository, "number", key.Number, "err", err)
		return domain.PartyResolution{Settings: toFeedPartySettings(resolver.rules.PartySettings(key.Repository, nil)), Fallback: true}
	}
	return domain.PartyResolution{Settings: toFeedPartySettings(resolver.rules.PartySettings(key.Repository, changedFiles))}
}

func (resolver *RoutingPartySettings) readChangedFiles(ctx context.Context, key domain.PullRequestKey) ([]string, error) {
	owner, name, found := strings.Cut(key.Repository, "/")
	if !found {
		return nil, domain.ErrMalformedRepository
	}
	readContext, cancel := context.WithTimeout(ctx, domain.PartyFilesReadTimeout)
	defer cancel()
	return resolver.files.ListPullRequestFiles(readContext, owner, name, key.Number)
}

func toFeedPartySettings(settings routingdomain.PartySettings) domain.PartySettings {
	return domain.PartySettings{Reviewers: settings.Reviewers, Deadline: settings.Deadline}
}
