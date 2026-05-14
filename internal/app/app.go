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
	"time"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/githubhook"
	"github.com/mptooling/notifycat/internal/pullrequest"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// Cleanup releases resources acquired by Wire (database connections, ...).
type Cleanup func()

// Wire builds the HTTP server (and its dependencies) from cfg. Callers run
// the returned server and invoke cleanup on shutdown.
//
// Server hardening defaults live here (timeouts, body limits). Adding a new
// event trigger means: write a new pullrequest.EventHandler, then add it to
// the dispatcher slice below — no other change in this file.
func Wire(cfg config.Config) (*http.Server, Cleanup, error) {
	logger := newLogger(cfg)

	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	if err := store.MigrateUp(context.Background(), db); err != nil {
		return nil, nil, fmt.Errorf("app: migrate: %w", err)
	}

	messages := store.NewSlackMessages(db)
	mappings := store.NewRepoMappings(db)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(httpClient, cfg.SlackBotToken.Reveal())
	composer := slack.NewComposer(cfg.Reactions.NewPR)

	dispatcher := pullrequest.NewDispatcher(
		pullrequest.NewOpenHandler(messages, mappings, slackClient, composer, logger),
		pullrequest.NewCloseHandler(messages, mappings, slackClient, composer, logger,
			pullrequest.CloseOptions{
				ReactionsEnabled: cfg.Reactions.Enabled,
				MergedEmoji:      cfg.Reactions.MergedPR,
				ClosedEmoji:      cfg.Reactions.ClosedPR,
			},
		),
		pullrequest.NewDraftHandler(messages, mappings, slackClient, logger),
		pullrequest.NewApproveHandler(messages, mappings, slackClient, logger, cfg.Reactions.Approved),
		pullrequest.NewCommentedHandler(messages, mappings, slackClient, logger, cfg.Reactions.Commented),
		pullrequest.NewRequestChangeHandler(messages, mappings, slackClient, logger, cfg.Reactions.RequestChange),
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

	cleanup := func() {
		if sqlDB, err := store.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}
	return server, cleanup, nil
}

func eventSink(d *pullrequest.Dispatcher, logger *slog.Logger) githubhook.EventSink {
	return func(ctx context.Context, p githubhook.Payload) error {
		event := pullrequest.Event{
			Action:     p.Action,
			Repository: p.Repository,
			PR: pullrequest.PR{
				Number: p.PullRequest.Number,
				Title:  p.PullRequest.Title,
				URL:    p.PullRequest.URL,
				Author: p.PullRequest.Author,
				Merged: p.PullRequest.Merged,
				Draft:  p.PullRequest.Draft,
			},
		}
		if p.Review != nil {
			event.Review = &pullrequest.Review{State: p.Review.State}
		}
		if err := d.Dispatch(ctx, event); err != nil {
			logger.Error("dispatch failed",
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
