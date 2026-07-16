package salience_test

import (
	"testing"

	"go.uber.org/fx"

	"github.com/mptooling/notifycat/internal/salience"
	"github.com/mptooling/notifycat/internal/salience/domain"
)

func TestModuleGraphResolves(t *testing.T) {
	app := fx.New(
		fx.Supply(domain.AdvisorParams{}),
		salience.Module,
		fx.Invoke(func(domain.Advisor) {}),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("salience module graph: %v", err)
	}
}
