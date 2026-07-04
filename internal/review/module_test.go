package review_test

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/slack"
	"github.com/mptooling/notifycat/internal/review"
	"github.com/mptooling/notifycat/internal/review/domain"
)

// TestModule_GraphResolves asserts review.Module builds the start-review use case
// and binds the code-reviews repo to both its own Recorder port and the
// notification ReviewSessions port.
func TestModule_GraphResolves(t *testing.T) {
	db := persistence.NewTestDB(t)

	app := fxtest.New(t,
		review.Module,
		fx.Provide(
			func() *persistence.CodeReviews { return persistence.NewCodeReviews(db) },
			func() *persistence.PullRequests { return persistence.NewPullRequests(db) },
			func() *slack.Composer { return slack.NewComposer("eyes") },
			func() *slack.Client { return slack.NewClient(http.DefaultClient, "xoxb-test") },
			func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
		),
		fx.Invoke(func(domain.StartReview, notificationdomain.ReviewSessions) {}),
	)

	app.RequireStart()
	app.RequireStop()
}
