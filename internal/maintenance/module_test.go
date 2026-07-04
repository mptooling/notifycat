package maintenance_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/mptooling/notifycat/internal/maintenance"
	"github.com/mptooling/notifycat/internal/maintenance/domain"
	"github.com/mptooling/notifycat/internal/maintenance/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/github"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// stubPRStateGetter stands in for the GitHub client so the module graph can be
// built without a network dependency.
type stubPRStateGetter struct{}

func (stubPRStateGetter) GetPullRequest(context.Context, string, string, int) (github.PullRequestState, error) {
	return github.PullRequestState{}, nil
}

// TestModule_GraphResolves asserts that maintenance.Module, given only the
// external inputs the composition root supplies, can build both use cases with
// every port bound. It proves the module honest without any production binary
// depending on it yet.
func TestModule_GraphResolves(t *testing.T) {
	db := persistence.NewTestDB(t)

	app := fxtest.New(t,
		maintenance.Module,
		fx.Provide(
			func() *persistence.PullRequests { return persistence.NewPullRequests(db) },
			func() infrastructure.PRStateGetter { return stubPRStateGetter{} },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Supply(maintenance.Config{TTL: 30 * 24 * time.Hour, DryRun: false}),
		fx.Invoke(func(domain.StaleMessageCleaner, domain.Reconciler) {}),
	)

	app.RequireStart()
	app.RequireStop()
}
