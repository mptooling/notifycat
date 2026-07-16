package openaicompat

import (
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// Module provides the OpenAI-compatible ModelGateway binding. The composition
// root appends exactly one provider module based on ai.provider.
var Module = fx.Module("salience-openaicompat",
	fx.Provide(fx.Annotate(NewClient, fx.As(new(domain.ModelGateway)))),
)
