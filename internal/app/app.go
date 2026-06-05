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
	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/githubhook"
	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
	"github.com/mptooling/notifycat/internal/validate"
)

// Cleanup releases resources acquired by Wire (database connections, ...).
type Cleanup func()

// Wire builds the HTTP server, the stale-row cleanup scheduler, and the
// shared cleanup func from cfg. Callers run the server and the scheduler in
// separate goroutines and invoke cleanup on shutdown.
//
// Mappings come from the declarative cfg.MappingsFile; the server refuses
// to start if any entry fails validation (against the per-entry lock cache).
func Wire(cfg config.Config) (*http.Server, *cleanup.Scheduler, Cleanup, error) {
	logger := newLogger(cfg)

	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("app: load mappings: %w", err)
	}

	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := store.MigrateUp(context.Background(), db); err != nil {
		return nil, nil, nil, fmt.Errorf("app: migrate: %w", err)
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	slackOpts := []slack.Option{}
	if cfg.SlackBaseURL != "" && cfg.SlackBaseURL != "https://slack.com" {
		slackOpts = append(slackOpts, slack.WithBaseURL(cfg.SlackBaseURL))
	}
	slackClient := slack.NewClient(httpClient, cfg.SlackBotToken.Reveal(), slackOpts...)
	composer := slack.NewComposer(cfg.Reactions.NewPR)

	if err := startupValidate(provider, cfg, slackClient, httpClient, logger); err != nil {
		closeDB(db)
		return nil, nil, nil, err
	}

	messages := store.NewSlackMessages(db)
	aiDetector := aireview.NewDetector(cfg.IgnoreAIReviews)
	scheduler := cleanup.NewScheduler(
		messages,
		time.Duration(cfg.MessageTTLDays)*24*time.Hour,
		cleanup.Interval,
		logger,
	)

	dispatcher := pullrequest.NewDispatcher(
		logger,
		pullrequest.NewOpenHandler(messages, provider, slackClient, composer, logger, cfg.DependabotFormat),
		pullrequest.NewCloseHandler(messages, provider, slackClient, composer, logger,
			pullrequest.CloseOptions{
				ReactionsEnabled: cfg.Reactions.Enabled,
				MergedEmoji:      cfg.Reactions.MergedPR,
				ClosedEmoji:      cfg.Reactions.ClosedPR,
			},
		),
		pullrequest.NewDraftHandler(messages, provider, slackClient, logger),
		pullrequest.NewApproveHandler(messages, provider, slackClient, logger, cfg.Reactions.Approved, cfg.Reactions.BotReview, aiDetector),
		pullrequest.NewCommentedHandler(messages, provider, slackClient, logger, cfg.Reactions.Commented, cfg.Reactions.BotReview, aiDetector),
		pullrequest.NewRequestChangeHandler(messages, provider, slackClient, logger, cfg.Reactions.RequestChange, cfg.Reactions.BotReview, aiDetector),
	)

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

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 14, // 16 KiB
	}

	return server, scheduler, func() { closeDB(db) }, nil
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
	lockPath := mappings.LockPath(cfg.MappingsFile)
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
