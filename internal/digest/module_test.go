package digest_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/mptooling/notifycat/internal/digest"
	"github.com/mptooling/notifycat/internal/digest/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// stubMappingLookup and stubDigestResolver stand in for the routing provider so
// the module graph can be built without loading a config.
type stubMappingLookup struct{}

func (stubMappingLookup) Get(context.Context, string) (routingdomain.RepoMapping, error) {
	return routingdomain.RepoMapping{}, routingdomain.ErrNotFound
}

type stubDigestResolver struct{}

func (stubDigestResolver) DigestFor(string) routingdomain.DigestConfig {
	return routingdomain.DigestConfig{}
}

// TestModule_GraphResolves asserts that digest.Module, given only the external
// inputs the composition root supplies, can build the reporter and scheduler use
// cases with every port bound. It proves the module honest without any
// production binary depending on it yet.
func TestModule_GraphResolves(t *testing.T) {
	db := store.NewTestDB(t)

	app := fxtest.New(t,
		digest.Module,
		fx.Provide(
			func() *store.PullRequests { return store.NewPullRequests(db) },
			func() *slack.Composer { return slack.NewComposer("eyes") },
			func() *slack.Client { return slack.NewClient(http.DefaultClient, "xoxb-test") },
			func() domain.MappingLookup { return stubMappingLookup{} },
			func() domain.DigestResolver { return stubDigestResolver{} },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Supply(digest.Config{Specs: []string{"0 9 * * *"}, TZ: time.UTC}),
		fx.Invoke(func(domain.DigestReporter, domain.DigestScheduler) {}),
	)

	app.RequireStart()
	app.RequireStop()
}
