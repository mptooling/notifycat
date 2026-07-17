package openaicompat

import (
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// Module is a convention-consistent fx wiring for the OpenAI-compatible
// ModelGateway. It mirrors the runtime's salienceGateway switch, which
// constructs the gateway directly via a switch on ai.provider; this module is
// not mounted by the composition root but is available for use in tests or
// alternative wiring.
var Module = fx.Module("salience-openaicompat",
	fx.Provide(fx.Annotate(NewClient, fx.As(new(domain.ModelGateway)))),
)
