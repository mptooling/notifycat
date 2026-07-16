package gemini

import (
	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// Module provides the Gemini ModelGateway binding. The composition root
// appends exactly one provider module based on ai.provider — with the feature
// off, no gateway is constructed at all.
var Module = fx.Module("salience-gemini",
	fx.Provide(fx.Annotate(NewClient, fx.As(new(domain.ModelGateway)))),
)
