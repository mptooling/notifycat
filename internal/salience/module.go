// Package salience wires the salience domain — the optional AI decision layer
// — into an fx module. This file is the only fx-aware part of the domain; the
// domain, application, and infrastructure layers stay framework-free. The
// composition root supplies domain.AdvisorParams (config, the selected
// provider gateway or nil, logger, clock); provider modules live under
// infrastructure/gemini and infrastructure/openaicompat, each self-contained.
package salience

import (
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/salience/application"
	"github.com/mptooling/notifycat/internal/salience/domain"
)

// Module binds the Advisor port: deterministic when disabled, resilient
// model-backed when enabled.
var Module = fx.Module("salience",
	fx.Provide(provideAdvisor),
)

// provideAdvisor builds the bound Advisor from the supplied params.
func provideAdvisor(params domain.AdvisorParams) domain.Advisor {
	return application.NewAdvisor(params)
}
