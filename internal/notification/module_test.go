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
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

type stubReviewSessions struct{}

func (stubReviewSessions) GetActive(context.Context, string, int) (domain.ReviewSession, error) {
	return domain.ReviewSession{}, domain.ErrNoActiveReview
}
func (stubReviewSessions) Finish(context.Context, string, int) error { return nil }
func (stubReviewSessions) Reviewers(context.Context, string, int) ([]domain.ReviewSession, error) {
	return nil, nil
}

type stubBehavior struct{}

func (stubBehavior) Get(context.Context, string) (routingdomain.RepoMapping, error) {
	return routingdomain.RepoMapping{}, nil
}

type stubResolver struct{}

func (stubResolver) ResolveTargets(context.Context, string, int) (routingdomain.ResolvedTargets, error) {
	return routingdomain.ResolvedTargets{}, nil
}

// TestModule_GraphResolves asserts that notification.Module, given only the
// external inputs the composition root supplies, builds the dispatcher with every
// handler assembled into the value group and every port bound.
func TestModule_GraphResolves(t *testing.T) {
	db := persistence.NewTestDB(t)

	app := fxtest.New(t,
		notification.Module,
		fx.Provide(
			func() *persistence.PullRequests { return persistence.NewPullRequests(db) },
			func() *persistence.CodeReviews { return persistence.NewCodeReviews(db) },
			func() *slack.Client { return slack.NewClient(http.DefaultClient, "xoxb-test") },
			func() *slack.Composer { return slack.NewComposer("eyes") },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
			func() domain.RepoBehavior { return stubBehavior{} },
			func() domain.TargetResolver { return stubResolver{} },
			func() domain.ReviewSessions { return stubReviewSessions{} },
		),
		fx.Invoke(func(domain.EventDispatcher) {}),
	)

	app.RequireStart()
	app.RequireStop()
}
