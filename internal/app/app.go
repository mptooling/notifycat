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

	"github.com/mptooling/notifycat/internal/aireview"
	"github.com/mptooling/notifycat/internal/cleanup"
	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/digest"
	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/githubhook"
	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/slackhook"
	"github.com/mptooling/notifycat/internal/startreview"
	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
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
func Wire(cfg config.Config) (*http.Server, *cleanup.Scheduler, *digest.Scheduler, Cleanup, error) {
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

	router := buildRouter(httpClient, cfg, provider, logger)
	dispatcher := buildDispatcher(pullRequests, provider, router, slackClient, composer, logger)

	codeReviews := store.NewCodeReviews(db)
	startReviewHandler := startreview.NewHandler(codeReviews, pullRequests, slackClient, composer, logger, time.Now)

	server := buildServer(cfg, buildMux(cfg, dispatcher, startReviewHandler.Handle, logger))
	return server, scheduler, digestScheduler, func() { closeDB(db) }, nil
}

// buildProvider constructs the mappings provider from config and warns when
// path routing is configured but no GitHub token is available to read a PR's
// changed files — path rules are then inert and PRs route to the repo tier.
func buildProvider(cfg config.Config, logger *slog.Logger) *mappings.Provider {
	defaults := mappings.Defaults{
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
	provider := mappings.NewProvider(defaults, cfg.Mappings, cfg.Digest)
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
func buildCleanupScheduler(cfg config.Config, pullRequests *store.PullRequests, logger *slog.Logger) *cleanup.Scheduler {
	return cleanup.NewScheduler(
		pullRequests,
		time.Duration(cfg.MessageTTLDays)*24*time.Hour,
		cleanup.Interval,
		logger,
	)
}

// buildDigestScheduler builds the stuck-PR digest scheduler, which fires on
// every distinct cron spec across the enabled mappings. It returns a nil
// scheduler (and nil error) when no mapping enables a digest; a bad cron spec
// fails startup here rather than silently never firing.
func buildDigestScheduler(cfg config.Config, provider *mappings.Provider, pullRequests *store.PullRequests, slackClient *slack.Client, composer *slack.Composer, logger *slog.Logger) (*digest.Scheduler, error) {
	specs := provider.Schedules()
	if len(specs) == 0 {
		return nil, nil
	}
	reporter := digest.NewReporter(pullRequests, provider, slackClient, composer, provider, logger, cfg.DigestTimezone)
	scheduler, err := digest.NewScheduler(specs, reporter, logger, cfg.DigestTimezone)
	if err != nil {
		return nil, fmt.Errorf("app: digest scheduler: %w", err)
	}
	return scheduler, nil
}

// buildRouter builds the per-PR target router. Path routing needs a GitHub
// token to read a PR's changed files; without one the router has no fetcher and
// resolves to the repo/org tier. The validation client is scoped to startup, so
// this builds a dedicated long-lived files fetcher.
func buildRouter(httpClient *http.Client, cfg config.Config, provider *mappings.Provider, logger *slog.Logger) *pullrequest.Router {
	var filesFetcher pullrequest.ChangedFiles
	if cfg.GitHubToken.Reveal() != "" {
		filesFetcher = github.NewClient(httpClient, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	return pullrequest.NewRouter(provider, filesFetcher, logger)
}

// buildDispatcher wires the PR-event handlers behind the dispatcher.
func buildDispatcher(pullRequests *store.PullRequests, provider *mappings.Provider, router *pullrequest.Router, slackClient *slack.Client, composer *slack.Composer, logger *slog.Logger) *pullrequest.Dispatcher {
	aiDetector := aireview.NewDetector()
	return pullrequest.NewDispatcher(
		logger,
		pullrequest.NewOpenHandler(pullRequests, router, slackClient, composer, logger),
		pullrequest.NewCloseHandler(pullRequests, provider, slackClient, composer, logger),
		pullrequest.NewDraftHandler(pullRequests, slackClient, logger),
		pullrequest.NewApproveHandler(pullRequests, provider, slackClient, logger, aiDetector),
		pullrequest.NewCommentedHandler(pullRequests, provider, slackClient, logger, aiDetector),
		pullrequest.NewRequestChangeHandler(pullRequests, provider, slackClient, logger, aiDetector),
	)
}

// buildMux builds the HTTP routes: health check, the GitHub webhook, and the
// optional inbound Slack interactivity endpoint. The Slack route is registered
// only when a signing secret is configured; otherwise notifycat stays
// outbound-only and the route is absent.
func buildMux(cfg config.Config, dispatcher *pullrequest.Dispatcher, startReviewSink slackhook.InteractionSink, logger *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	verifier := githubhook.NewVerifier(cfg.GitHubWebhookSecret.Reveal())
	mux.Handle("POST /webhook/github",
		githubhook.SignatureMiddleware(verifier)(
			githubhook.NewHandler(eventSink(dispatcher, logger)),
		),
	)

	if cfg.SlackSigningSecret.Reveal() == "" {
		logger.Info("slack interactivity disabled", slog.String("reason", "SLACK_SIGNING_SECRET unset"))
	} else {
		slackVerifier := slackhook.NewVerifier(cfg.SlackSigningSecret.Reveal())
		mux.Handle("POST /webhook/slack/interactions",
			slackhook.SignatureMiddleware(slackVerifier)(
				slackhook.NewHandler(startReviewSink, logger),
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
	provider *mappings.Provider,
	cfg config.Config,
	slackClient *slack.Client,
	httpClient *http.Client,
	logger *slog.Logger,
) error {
	entries := provider.Entries()
	if len(entries) == 0 {
		return nil
	}
	lockPath := mappings.LockPath(cfg.ConfigFile)
	lock, err := mappings.ReadLock(lockPath)
	if err != nil {
		logger.Warn("startup validate: lock unreadable; rebuilding", slog.Any("err", err))
		lock = mappings.Lock{Version: mappings.LockVersion, Entries: map[string]mappings.LockEntry{}}
	}
	diff := mappings.DiffEntries(entries, lock)
	if len(diff.Needs) == 0 {
		return persistLock(lockPath, lock, nil, diff.Stale, logger)
	}

	checker, lister := newValidationDeps(provider, cfg, slackClient, httpClient)
	results := validate.RunForEntries(context.Background(), diff.Needs, lister, checker)
	successes, failed := splitResults(results, time.Now)
	_ = persistLock(lockPath, lock, successes, diff.Stale, logger)
	if len(failed) == 0 {
		return nil
	}
	logFailures(results, logger)
	return fmt.Errorf("app: startup validation failed for %d entries: %s", len(failed), strings.Join(failed, ", "))
}

func newValidationDeps(provider *mappings.Provider, cfg config.Config, slackClient *slack.Client, httpClient *http.Client) (validate.RepoValidator, validate.OrgRepoLister) {
	var ghChecker validate.GitHubChecker
	var lister validate.OrgRepoLister
	if cfg.GitHubToken.Reveal() != "" {
		gh := github.NewClient(httpClient, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
		ghChecker = gh
		lister = gh
	}
	return validate.NewValidator(provider, slackClient, ghChecker), lister
}

func splitResults(results []validate.EntryResult, clock func() time.Time) (map[string]mappings.LockEntry, []string) {
	successes := map[string]mappings.LockEntry{}
	var failed []string
	for _, r := range results {
		if r.OK() {
			successes[r.Entry.Key()] = mappings.LockEntry{SHA256: r.Entry.Hash(), ValidatedAt: clock()}
			continue
		}
		failed = append(failed, r.Entry.Key())
	}
	return successes, failed
}

func persistLock(lockPath string, lock mappings.Lock, ok map[string]mappings.LockEntry, stale []string, logger *slog.Logger) error {
	merged := mappings.MergeLock(lock, ok, stale)
	if err := mappings.WriteLock(lockPath, merged); err != nil {
		logger.Warn("startup validate: write lock failed", slog.Any("err", err))
		return nil
	}
	return nil
}

func logFailures(results []validate.EntryResult, logger *slog.Logger) {
	for _, r := range results {
		if r.OK() {
			continue
		}
		for _, rep := range r.Reports {
			for _, c := range rep.Checks {
				if c.Status != validate.StatusFail {
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

func eventSink(d *pullrequest.Dispatcher, logger *slog.Logger) githubhook.EventSink {
	return func(ctx context.Context, p githubhook.Payload) error {
		event := pullrequest.Event{
			GitHubEvent: p.Event,
			Action:      p.Action,
			Repository:  p.Repository,
			PR: pullrequest.PR{
				Number:    p.PullRequest.Number,
				Title:     p.PullRequest.Title,
				URL:       p.PullRequest.URL,
				Author:    p.PullRequest.Author,
				Merged:    p.PullRequest.Merged,
				Draft:     p.PullRequest.Draft,
				Body:      p.PullRequest.Body,
				CreatedAt: p.PullRequest.CreatedAt,
			},
			PRComment: p.PRComment,
			Sender:    pullrequest.Sender{Login: p.Sender.Login, Type: p.Sender.Type},
		}
		if p.Review != nil {
			event.Review = &pullrequest.Review{State: p.Review.State}
		}
		if err := d.Dispatch(ctx, event); err != nil {
			logger.Error("dispatch failed",
				slog.String("github_event", event.GitHubEvent),
				slog.String("repository", event.Repository),
				slog.Int("pr", event.PR.Number),
				slog.String("action", event.Action),
				slog.Any("err", err),
			)
			return err
		}
		return nil
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
