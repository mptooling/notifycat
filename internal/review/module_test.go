package review_test

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/review"
	"github.com/mptooling/notifycat/internal/review/domain"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// TestModule_GraphResolves asserts review.Module builds the start-review use case
// and binds the code-reviews repo to both its own Recorder port and the
// notification ReviewSessions port.
func TestModule_GraphResolves(t *testing.T) {
	db := store.NewTestDB(t)

	app := fxtest.New(t,
		review.Module,
		fx.Provide(
			func() *store.CodeReviews { return store.NewCodeReviews(db) },
			func() *store.PullRequests { return store.NewPullRequests(db) },
			func() *slack.Composer { return slack.NewComposer("eyes") },
			func() *slack.Client { return slack.NewClient(http.DefaultClient, "xoxb-test") },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Invoke(func(domain.StartReview, notificationdomain.ReviewSessions) {}),
	)

	app.RequireStart()
	app.RequireStop()
}
