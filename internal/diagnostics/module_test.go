package diagnostics_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/mptooling/notifycat/internal/diagnostics"
	diagnosticsapp "github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// stubRepoValidator satisfies validationdomain.RepoValidator with no-op behaviour.
type stubRepoValidator struct{}

func (stubRepoValidator) Validate(_ context.Context, repository string) validationdomain.Report {
	return validationdomain.Report{Repository: repository}
}

// TestModule_GraphResolves asserts that diagnostics.Module, given only the
// external inputs the composition root supplies, can build all three use cases
// with every port bound.
func TestModule_GraphResolves(t *testing.T) {
	db := persistence.NewTestDB(t)
	pullRequests := persistence.NewPullRequests(db)

	// A zero-entry Provider satisfies both EntrySource and RoutingProvider.
	provider := routingapp.NewProvider(routingdomain.Defaults{}, nil, nil)

	lockPath := t.TempDir() + "/config.lock"

	cfg := diagnostics.Config{
		ConfigSnapshot: diagnosticsdomain.ConfigSnapshot{},
		SmokeConfig: diagnosticsdomain.SmokeConfig{
			Now: time.Now,
		},
		LockPath: lockPath,
		Clock:    time.Now,
	}

	app := fxtest.New(t,
		diagnostics.Module,

		// External inputs supplied by the composition root.
		fx.Provide(
			func() *persistence.PullRequests { return pullRequests },
			func() *slack.Client { return slack.NewClient(http.DefaultClient, "xoxb-test") },
			func() *http.Client { return http.DefaultClient },
			func() routingdomain.RoutingProvider { return provider },
			func() diagnosticsdomain.EntrySource { return provider },
			func() validationdomain.RepoValidator { return stubRepoValidator{} },
			func() validationdomain.OrgRepoLister { return nil },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Supply(cfg),

		fx.Invoke(func(
			_ diagnosticsdomain.Doctor,
			_ diagnosticsdomain.Smoke,
			_ *diagnosticsapp.MappingsValidator,
		) {
		}),
	)

	app.RequireStart()
	app.RequireStop()
}
