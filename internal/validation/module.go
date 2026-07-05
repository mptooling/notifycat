// Package validation wires the validation domain — per-repository mapping
// checks against Slack and a git provider — into an fx module. This file is the
// only fx-aware part of the domain; the domain, application, and infrastructure
// layers stay framework-free.
package validation

import (
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/validation/application"
	"github.com/mptooling/notifycat/internal/validation/domain"
	"github.com/mptooling/notifycat/internal/validation/infrastructure"
)

// Module binds the validation ports to their implementations: the Slack probe
// adapter satisfies the SlackChecker port, and the application Validator
// satisfies the RepoValidator use-case port. The composition root supplies the
// external inputs the module cannot build itself — a *slack.Client for the
// probe, the MappingLookup port (the routing provider), and the provider-neutral
// HookProbe (the git-provider hook checker plus its URL suffix and required
// events).
var Module = fx.Module("validation",
	fx.Provide(
		fx.Annotate(infrastructure.NewSlackProbe, fx.As(new(domain.SlackChecker))),
		fx.Annotate(application.NewValidator, fx.As(new(domain.RepoValidator))),
	),
)
