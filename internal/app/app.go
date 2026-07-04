// Package app is the composition root for notifycat. It wires config,
// database, Slack client, event handlers, and HTTP routes into a single
// runnable server.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/config"
	digestapp "github.com/mptooling/notifycat/internal/digest/application"
	digestdomain "github.com/mptooling/notifycat/internal/digest/domain"
	digestinfra "github.com/mptooling/notifycat/internal/digest/infrastructure"
	"github.com/mptooling/notifycat/internal/github"
	maintenanceapp "github.com/mptooling/notifycat/internal/maintenance/application"
	maintenancedomain "github.com/mptooling/notifycat/internal/maintenance/domain"
	maintenanceinfra "github.com/mptooling/notifycat/internal/maintenance/infrastructure"
	notificationapp "github.com/mptooling/notifycat/internal/notification/application"
	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	notificationinfra "github.com/mptooling/notifycat/internal/notification/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/security"
	reviewapp "github.com/mptooling/notifycat/internal/review/application"
	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
	reviewinfra "github.com/mptooling/notifycat/internal/review/infrastructure"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
	validationapp "github.com/mptooling/notifycat/internal/validation/application"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
	validationinfra "github.com/mptooling/notifycat/internal/validation/infrastructure"
)

// Cleanup releases resources acquired by Wire (database connections, ...).
type Cleanup func()

// Wire builds the HTTP server, the stale-row cleanup scheduler, the stuck-PR
// digest scheduler (nil when the digest is disabled), and the shared cleanup
// func from cfg. Callers run the server and both schedulers in separate
// goroutines and invoke cleanup on shutdown.
//
// Mappings come from the `mappings:` section of config.yaml; the server
// refuses to start if any entry fails validation (against the per-entry lock).
func Wire(cfg config.Config) (*http.Server, *maintenanceapp.Cleaner, *digestapp.Scheduler, Cleanup, error) {
	logger := newLogger(cfg)
	provider := buildProvider(cfg, logger)

	db, err := openAndMigrate(cfg)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	slackClient := buildSlackClient(httpClient, cfg)
	composer := slack.NewComposer(cfg.Reactions.NewPR)

	if err := startupValidate(provider, cfg, slackClient, httpClient, logger); err != nil {
		closeDB(db)
		return nil, nil, nil, nil, err
	}

	pullRequests := store.NewPullRequests(db)
	scheduler := buildCleanupScheduler(cfg, pullRequests, logger)

	digestScheduler, err := buildDigestScheduler(cfg, provider, pullRequests, slackClient, composer, logger)
	if err != nil {
		closeDB(db)
		return nil, nil, nil, nil, err
	}

	codeReviews := store.NewCodeReviews(db)

	router := buildRouter(httpClient, cfg, provider, logger)
	dispatcher := buildDispatcher(pullRequests, codeReviews, provider, router, slackClient, composer, logger)

	startReview := reviewapp.NewHandler(reviewdomain.HandlerParams{
		Recorder:  reviewinfra.NewCodeReviewsRepo(codeReviews),
		Messages:  reviewinfra.NewMessageChecker(pullRequests),
		Decorator: reviewinfra.NewSlackDecorator(composer, slackClient),
		Logger:    logger,
		Now:       time.Now,
	})
	startReviewSink := reviewinfra.NewStartReviewSink(startReview, logger)

	server := buildServer(cfg, buildMux(cfg, dispatcher, startReviewSink, logger))
	return server, scheduler, digestScheduler, func() { closeDB(db) }, nil
}

// buildProvider constructs the mappings provider from config and warns when
// path routing is configured but no GitHub token is available to read a PR's
// changed files — path rules are then inert and PRs route to the repo tier.
func buildProvider(cfg config.Config, logger *slog.Logger) *routingapp.Provider {
	defaults := routingdomain.Defaults{
		Reactions: store.Reactions{
			Enabled:       cfg.Reactions.Enabled,
			NewPR:         cfg.Reactions.NewPR,
			MergedPR:      cfg.Reactions.MergedPR,
			ClosedPR:      cfg.Reactions.ClosedPR,
			Approved:      cfg.Reactions.Approved,
			Commented:     cfg.Reactions.Commented,
			RequestChange: cfg.Reactions.RequestChange,
			BotReview:     cfg.Reactions.BotReview,
		},
		IgnoreAIReviews:  cfg.IgnoreAIReviews,
		DependabotFormat: cfg.DependabotFormat,
	}
	provider := routingapp.NewProvider(defaults, cfg.Mappings, cfg.Digest)
	if provider.HasPathRules() && cfg.GitHubToken.Reveal() == "" {
		logger.Warn("path routing is configured but GITHUB_TOKEN is unset; " +
			"path rules are inert and PRs route to the repo tier (a token is needed to read a PR's changed files)")
	}
	return provider
}

// openAndMigrate opens the database and applies pending migrations. On any
// failure it releases the handle, so the caller never holds an unclosed db.
func openAndMigrate(cfg config.Config) (*gorm.DB, error) {
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := store.MigrateUp(context.Background(), db); err != nil {
		closeDB(db)
		return nil, fmt.Errorf("app: migrate: %w", err)
	}
	return db, nil
}

// buildSlackClient builds the Slack API client, overriding the base URL only
// when the operator set a non-default one (tests point it at a fake server).
func buildSlackClient(httpClient *http.Client, cfg config.Config) *slack.Client {
	slackOpts := []slack.Option{}
	if cfg.SlackBaseURL != "" && cfg.SlackBaseURL != "https://slack.com" {
		slackOpts = append(slackOpts, slack.WithBaseURL(cfg.SlackBaseURL))
	}
	return slack.NewClient(httpClient, cfg.SlackBotToken.Reveal(), slackOpts...)
}

// buildCleanupScheduler builds the scheduler that deletes stored-message rows
// older than the configured TTL.
func buildCleanupScheduler(cfg config.Config, pullRequests *store.PullRequests, logger *slog.Logger) *maintenanceapp.Cleaner {
	return maintenanceapp.NewCleaner(maintenancedomain.CleanerParams{
		Deleter:  maintenanceinfra.NewPRRepository(pullRequests),
		TTL:      time.Duration(cfg.MessageTTLDays) * 24 * time.Hour,
		Interval: maintenancedomain.Interval,
		Logger:   logger,
		Now:      time.Now,
	})
}

// buildDigestScheduler builds the stuck-PR digest scheduler, which fires on
// every distinct cron spec across the enabled mappings. It returns a nil
// scheduler (and nil error) when no mapping enables a digest; a bad cron spec
// fails startup here rather than silently never firing.
func buildDigestScheduler(cfg config.Config, provider *routingapp.Provider, pullRequests *store.PullRequests, slackClient *slack.Client, composer *slack.Composer, logger *slog.Logger) (*digestapp.Scheduler, error) {
	specs := provider.Schedules()
	if len(specs) == 0 {
		return nil, nil
	}
	reporter := digestapp.NewReporter(digestdomain.ReporterParams{
		Finder:   digestinfra.NewStuckRepo(pullRequests),
		Mappings: provider,
		Poster:   digestinfra.NewSlackPoster(slackClient),
		Composer: digestinfra.NewSlackComposer(composer),
		Digests:  provider,
		Logger:   logger,
		TZ:       cfg.DigestTimezone,
		Now:      time.Now,
	})
	scheduler, err := digestapp.NewScheduler(digestdomain.SchedulerParams{
		Specs:  specs,
		Job:    reporter,
		Logger: logger,
		TZ:     cfg.DigestTimezone,
	})
	if err != nil {
		return nil, fmt.Errorf("app: digest scheduler: %w", err)
	}
	return scheduler, nil
}

// buildRouter builds the per-PR target router. Path routing needs a GitHub
// token to read a PR's changed files; without one the router has no fetcher and
// resolves to the repo/org tier. The validation client is scoped to startup, so
// this builds a dedicated long-lived files fetcher.
func buildRouter(httpClient *http.Client, cfg config.Config, provider *routingapp.Provider, logger *slog.Logger) *routingapp.Router {
	var filesFetcher routingdomain.ChangedFilesReader
	if cfg.GitHubToken.Reveal() != "" {
		filesFetcher = github.NewClient(httpClient, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	return routingapp.NewRouter(provider, filesFetcher, logger)
}

// buildDispatcher wires the PR-event handlers behind the dispatcher.
func buildDispatcher(pullRequests *store.PullRequests, codeReviews *store.CodeReviews, provider *routingapp.Provider, router *routingapp.Router, slackClient *slack.Client, composer *slack.Composer, logger *slog.Logger) *notificationapp.Dispatcher {
	messageStore := notificationinfra.NewMessageRepo(pullRequests)
	messenger := notificationinfra.NewSlackMessenger(slackClient, composer)
	reviews := reviewinfra.NewCodeReviewsRepo(codeReviews)
	handlers := []notificationdomain.Handler{
		notificationapp.NewOpenHandler(messageStore, router, messenger, logger),
		notificationapp.NewCloseHandler(messageStore, provider, messenger, logger, reviews),
		notificationapp.NewDraftHandler(messageStore, messenger, logger),
		notificationapp.NewApproveHandler(messageStore, provider, messenger, logger, reviews),
		notificationapp.NewCommentedHandler(messageStore, provider, messenger, logger, reviews),
		notificationapp.NewRequestChangeHandler(messageStore, provider, messenger, logger, reviews),
	}
	return notificationapp.NewDispatcher(logger, handlers)
}

// buildMux builds the HTTP routes: health check, the GitHub webhook, and the
// optional inbound Slack interactivity endpoint. The Slack route is registered
// only when a signing secret is configured; otherwise notifycat stays
// outbound-only and the route is absent.
func buildMux(cfg config.Config, dispatcher *notificationapp.Dispatcher, startReviewSink reviewinfra.InteractionSink, logger *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	verifier := security.NewGitHubVerifier(cfg.GitHubWebhookSecret.Reveal())
	mux.Handle("POST /webhook/github",
		notificationinfra.SignatureMiddleware(verifier)(
			notificationinfra.NewGitHubHandler(dispatcher),
		),
	)

	if cfg.SlackSigningSecret.Reveal() == "" {
		logger.Info("slack interactivity disabled", slog.String("reason", "SLACK_SIGNING_SECRET unset"))
	} else {
		slackVerifier := security.NewSlackVerifier(cfg.SlackSigningSecret.Reveal())
		mux.Handle("POST /webhook/slack/interactions",
			reviewinfra.SignatureMiddleware(slackVerifier)(
				reviewinfra.NewInteractionsHandler(startReviewSink, logger),
			),
		)
		logger.Info("slack interactivity enabled", slog.String("route", "POST /webhook/slack/interactions"))
	}
	return mux
}

// buildServer builds the HTTP server with notifycat's hardened timeouts.
func buildServer(cfg config.Config, mux *http.ServeMux) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 14, // 16 KiB
	}
}

// startupValidate runs the cache-aware validation pipeline before the
// server begins serving. It validates only entries whose hash differs
// from the lock, writes the lock on success (failures keep their old
// hashes), and refuses to start if any entry fails.
func startupValidate(
	provider *routingapp.Provider,
	cfg config.Config,
	slackClient *slack.Client,
	httpClient *http.Client,
	logger *slog.Logger,
) error {
	entries := provider.Entries()
	if len(entries) == 0 {
		return nil
	}
	lockPath := routinginfra.LockPath(cfg.ConfigFile)
	lock, err := routinginfra.ReadLock(lockPath)
	if err != nil {
		logger.Warn("startup validate: lock unreadable; rebuilding", slog.Any("err", err))
		lock = routinginfra.Lock{Version: routinginfra.LockVersion, Entries: map[string]routinginfra.LockEntry{}}
	}
	diff := routinginfra.DiffEntries(entries, lock)
	if len(diff.Needs) == 0 {
		return persistLock(lockPath, lock, nil, diff.Stale, logger)
	}

	checker, lister := newValidationDeps(provider, cfg, slackClient, httpClient)
	results := validationapp.RunForEntries(context.Background(), diff.Needs, lister, checker)
	successes, failed := splitResults(results, time.Now)
	_ = persistLock(lockPath, lock, successes, diff.Stale, logger)
	if len(failed) == 0 {
		return nil
	}
	logFailures(results, logger)
	return fmt.Errorf("app: startup validation failed for %d entries: %s", len(failed), strings.Join(failed, ", "))
}

func newValidationDeps(provider *routingapp.Provider, cfg config.Config, slackClient *slack.Client, httpClient *http.Client) (validationdomain.RepoValidator, validationdomain.OrgRepoLister) {
	var ghChecker validationdomain.GitHubChecker
	var lister validationdomain.OrgRepoLister
	if cfg.GitHubToken.Reveal() != "" {
		gh := github.NewClient(httpClient, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
		ghChecker = gh
		lister = gh
	}
	return validationapp.NewValidator(provider, validationinfra.NewSlackProbe(slackClient), ghChecker), lister
}

func splitResults(results []validationdomain.EntryResult, clock func() time.Time) (map[string]routinginfra.LockEntry, []string) {
	successes := map[string]routinginfra.LockEntry{}
	var failed []string
	for _, r := range results {
		if r.OK() {
			successes[r.Entry.Key()] = routinginfra.LockEntry{SHA256: r.Entry.Hash(), ValidatedAt: clock()}
			continue
		}
		failed = append(failed, r.Entry.Key())
	}
	return successes, failed
}

func persistLock(lockPath string, lock routinginfra.Lock, ok map[string]routinginfra.LockEntry, stale []string, logger *slog.Logger) error {
	merged := routinginfra.MergeLock(lock, ok, stale)
	if err := routinginfra.WriteLock(lockPath, merged); err != nil {
		logger.Warn("startup validate: write lock failed", slog.Any("err", err))
		return nil
	}
	return nil
}

func logFailures(results []validationdomain.EntryResult, logger *slog.Logger) {
	for _, r := range results {
		if r.OK() {
			continue
		}
		for _, rep := range r.Reports {
			for _, c := range rep.Checks {
				if c.Status != validationdomain.StatusFail {
					continue
				}
				logger.Error("startup validate failure",
					slog.String("entry", r.Entry.Key()),
					slog.String("repository", rep.Repository),
					slog.String("check", c.Name),
					slog.String("detail", c.Detail))
			}
		}
	}
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := store.SQLDB(db); err == nil {
		_ = sqlDB.Close()
	}
}

func newLogger(cfg config.Config) *slog.Logger {
	level := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch cfg.LogFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
