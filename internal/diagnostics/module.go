// Package diagnostics wires the diagnostics domain — doctor preflight checks,
// config-CLI validation, and smoke delivery tests — into an fx module. This
// file is the only fx-aware part of the domain; the domain, application, and
// infrastructure layers stay framework-free.
package diagnostics

import (
	"time"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	diagnosticsinfra "github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/store"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// Config carries the composition-root-supplied, config-derived values the
// diagnostics use cases need but the module cannot build itself.
type Config struct {
	ConfigSnapshot diagnosticsdomain.ConfigSnapshot
	SmokeConfig    diagnosticsdomain.SmokeConfig
	LockPath       string
	Clock          func() time.Time
}

// Module binds the diagnostics ports to their implementations. The composition
// root must supply the following external inputs:
//   - *store.PullRequests
//   - *slack.Client
//   - *http.Client
//   - routingdomain.RoutingProvider (satisfies SmokeMappings Get)
//   - diagnosticsdomain.EntrySource (the routing provider for MappingsValidator)
//   - validationdomain.RepoValidator
//   - validationdomain.OrgRepoLister (may be nil via a provider func)
//   - diagnostics.Config
var Module = fx.Module("diagnostics",
	fx.Provide(
		// Infrastructure adapters bound to their domain ports.
		fx.Annotate(diagnosticsinfra.NewGitHubSigner, fx.As(new(diagnosticsdomain.Signer))),
		fx.Annotate(diagnosticsinfra.NewHTTPWebhookSender, fx.As(new(diagnosticsdomain.WebhookSender))),
		fx.Annotate(diagnosticsinfra.NewSlackSmokeReactions, fx.As(new(diagnosticsdomain.SmokeReactions))),
		fx.Annotate(diagnosticsinfra.NewStoreSmokeMessages, fx.As(new(diagnosticsdomain.SmokeMessages))),
		fx.Annotate(diagnosticsinfra.NewStoreSmokeCleanup, fx.As(new(diagnosticsdomain.SmokeCleanup))),

		// SmokeMappings adapter: wraps the RoutingProvider's Get method.
		fx.Annotate(provideSmokeMappings, fx.As(new(diagnosticsdomain.SmokeMappings))),

		// LockGateway reads LockPath and Clock from Config.
		fx.Annotate(provideLockGateway, fx.As(new(diagnosticsdomain.LockGateway))),

		// Use cases bound to their domain ports.
		fx.Annotate(provideDoctor, fx.As(new(diagnosticsdomain.Doctor))),
		fx.Annotate(provideSmokeUseCase, fx.As(new(diagnosticsdomain.Smoke))),

		// MappingsValidator provided as its concrete type (no domain interface).
		provideMappingsValidator,
	),
)

// provideSmokeMappings wraps the RoutingProvider — which satisfies the
// unexported smokeRoutingProvider interface in the infra package — as the
// SmokeMappings adapter.
func provideSmokeMappings(provider routingdomain.RoutingProvider) *diagnosticsinfra.MappingsSmokeMappings {
	return diagnosticsinfra.NewMappingsSmokeMappings(provider)
}

func provideLockGateway(cfg Config) *diagnosticsinfra.LockGateway {
	return diagnosticsinfra.NewLockGateway(cfg.LockPath, cfg.Clock)
}

func provideDoctor(cfg Config, validator validationdomain.RepoValidator) *application.Doctor {
	return application.NewDoctor(cfg.ConfigSnapshot, validator)
}

func provideSmokeUseCase(
	mappings diagnosticsdomain.SmokeMappings,
	messages diagnosticsdomain.SmokeMessages,
	reactions diagnosticsdomain.SmokeReactions,
	cleanup diagnosticsdomain.SmokeCleanup,
	signer diagnosticsdomain.Signer,
	sender diagnosticsdomain.WebhookSender,
	cfg Config,
) *application.SmokeUseCase {
	return application.NewSmokeUseCase(mappings, messages, reactions, cleanup, signer, sender, cfg.SmokeConfig)
}

func provideMappingsValidator(
	entries diagnosticsdomain.EntrySource,
	checker validationdomain.RepoValidator,
	lister validationdomain.OrgRepoLister,
	gateway diagnosticsdomain.LockGateway,
) *application.MappingsValidator {
	return application.NewMappingsValidator(entries, checker, lister, gateway)
}

// The module resolves *slack.Client and *store.PullRequests transitively via the
// infrastructure adapters; these blank assignments keep the imports live for
// callers that grep for which concrete types the module requires.
var (
	_ *slack.Client       = nil
	_ *store.PullRequests = nil
)
