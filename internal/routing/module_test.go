package routing_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/mptooling/notifycat/internal/routing"
	"github.com/mptooling/notifycat/internal/routing/domain"
)

// stubChangedFiles stands in for the GitHub client so the module graph can be
// built without a network dependency.
type stubChangedFiles struct{}

func (stubChangedFiles) ListPullRequestFiles(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}

// TestModule_GraphResolves asserts that routing.Module, given only the external
// inputs the composition root supplies, can build the provider and router with
// every port bound.
func TestModule_GraphResolves(t *testing.T) {
	app := fxtest.New(t,
		routing.Module,
		fx.Provide(func() domain.ChangedFilesReader { return stubChangedFiles{} }),
		fx.Provide(func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }),
		fx.Supply(routing.Config{
			Defaults: domain.Defaults{},
			Mappings: map[string]domain.Org{},
			Digest:   nil,
		}),
		fx.Invoke(func(domain.RoutingProvider, domain.TargetResolver) {}),
	)

	app.RequireStart()
	app.RequireStop()
}
