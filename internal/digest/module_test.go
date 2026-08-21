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
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
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
	db := persistence.NewTestDB(t)

	app := fxtest.New(t,
		digest.Module,
		fx.Provide(
			func() *persistence.PullRequests { return persistence.NewPullRequests(db) },
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

// digestGraphDeps are the external inputs digest.Module cannot build itself.
func digestGraphDeps(t *testing.T) fx.Option {
	t.Helper()
	db := persistence.NewTestDB(t)
	return fx.Provide(
		func() *persistence.PullRequests { return persistence.NewPullRequests(db) },
		func() *slack.Composer { return slack.NewComposer("eyes") },
		func() *slack.Client { return slack.NewClient(http.DefaultClient, "xoxb-test") },
		func() domain.MappingLookup { return stubMappingLookup{} },
		func() domain.DigestResolver { return stubDigestResolver{} },
		func() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) },
	)
}

func TestModule_ResolvesCalendarForConfiguredCountry(t *testing.T) {
	app := fxtest.New(t,
		digest.Module,
		digestGraphDeps(t),
		fx.Supply(digest.Config{Specs: []string{"0 9 * * *"}, TZ: time.UTC, Country: "DE"}),
		fx.Invoke(func(calendar domain.DigestCalendar) {
			// 2026-12-25 is a Friday and a German public holiday.
			if reason, skip := calendar.SkipReason(time.Date(2026, time.December, 25, 9, 0, 0, 0, time.UTC)); !skip || reason != domain.SkipReasonHoliday {
				t.Errorf("calendar not wired to the configured country: reason=%q skip=%v", reason, skip)
			}
		}),
	)
	app.RequireStart()
	app.RequireStop()
}

// An unknown country code must NOT abort startup. The digest is one feature and
// the code is a cosmetic setting, so a typo degrades to weekends-only rather
// than taking the whole deployment down.
func TestModule_UnknownCountryStartsAndDegrades(t *testing.T) {
	app := fxtest.New(t,
		digest.Module,
		digestGraphDeps(t),
		fx.Supply(digest.Config{Specs: []string{"0 9 * * *"}, TZ: time.UTC, Country: "ZZ"}),
		fx.Invoke(func(calendar domain.DigestCalendar) {
			if calendar.Country() != "" {
				t.Errorf("Country() = %q; an unrecognized code must resolve to none", calendar.Country())
			}
			// 2026-12-25 is a Friday: no holiday table, so the digest still posts.
			if reason, skip := calendar.SkipReason(time.Date(2026, time.December, 25, 9, 0, 0, 0, time.UTC)); skip {
				t.Errorf("2026-12-25 skipped as %q; want no skip without a usable table", reason)
			}
			// Saturday is still skipped.
			if reason, skip := calendar.SkipReason(time.Date(2026, time.July, 4, 9, 0, 0, 0, time.UTC)); !skip || reason != domain.SkipReasonWeekend {
				t.Errorf("Saturday: reason=%q skip=%v; want %q true", reason, skip, domain.SkipReasonWeekend)
			}
		}),
	)
	app.RequireStart()
	app.RequireStop()
}
