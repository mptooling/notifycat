package notification_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/mptooling/notifycat/internal/notification"
	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

type stubBehavior struct{}

func (stubBehavior) Get(context.Context, string) (routingdomain.RepoMapping, error) {
	return routingdomain.RepoMapping{}, nil
}

type stubResolver struct{}

func (stubResolver) ResolveTargets(context.Context, string, int) (routingdomain.RepoMapping, []routingdomain.Target, error) {
	return routingdomain.RepoMapping{}, nil, nil
}

// TestModule_GraphResolves asserts that notification.Module, given only the
// external inputs the composition root supplies, builds the dispatcher with every
// handler assembled into the value group and every port bound.
func TestModule_GraphResolves(t *testing.T) {
	db := store.NewTestDB(t)

	app := fxtest.New(t,
		notification.Module,
		fx.Provide(
			func() *store.PullRequests { return store.NewPullRequests(db) },
			func() *store.CodeReviews { return store.NewCodeReviews(db) },
			func() *slack.Client { return slack.NewClient(http.DefaultClient, "xoxb-test") },
			func() *slack.Composer { return slack.NewComposer("eyes") },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
			func() domain.RepoBehavior { return stubBehavior{} },
			func() domain.TargetResolver { return stubResolver{} },
		),
		fx.Invoke(func(domain.EventDispatcher) {}),
	)

	app.RequireStart()
	app.RequireStop()
}
