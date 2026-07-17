package gemini

import (
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// Module is a convention-consistent fx wiring for the Gemini ModelGateway. It
// mirrors the runtime's salienceGateway switch, which constructs the gateway
// directly via a switch on ai.provider; this module is not mounted by the
// composition root but is available for use in tests or alternative wiring.
var Module = fx.Module("salience-gemini",
	fx.Provide(fx.Annotate(NewClient, fx.As(new(domain.ModelGateway)))),
)
