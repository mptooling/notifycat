// Package runtime is the fx composition root for the notifycat server. It
// reproduces the legacy internal/app.Wire graph as a single fx.Module: every
// helper that Wire used to call is a provider here, the startup validation gate
// is an fx.Invoke that fails App.Start on any failing mapping, and the HTTP
// server plus the cleanup/digest schedulers are driven by fx.Lifecycle hooks.
//
// The module derives everything from a single config.Config the caller supplies
// (main does fx.Supply(cfg)); it never calls config.Load itself. As the
// composition root it is exempt from the inward-only layering rule and may
// import config, persistence, slack, github, and every domain's application and
// infrastructure packages.
package runtime //nolint:revive // the composition-root package is deliberately named "runtime" (referenced as runtime.Module); it is never imported alongside the stdlib runtime package

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"go.uber.org/fx"
	"gorm.io/gorm"

	digestapp "github.com/mptooling/notifycat/internal/digest/application"
	digestdomain "github.com/mptooling/notifycat/internal/digest/domain"
	digestinfra "github.com/mptooling/notifycat/internal/digest/infrastructure"
	"github.com/mptooling/notifycat/internal/kernel"
	maintenanceapp "github.com/mptooling/notifycat/internal/maintenance/application"
	maintenancedomain "github.com/mptooling/notifycat/internal/maintenance/domain"
	maintenanceinfra "github.com/mptooling/notifycat/internal/maintenance/infrastructure"
	notificationapp "github.com/mptooling/notifycat/internal/notification/application"
	notificationdomain "github.com/mptooling/notifycat/internal/notification/domain"
	notificationinfra "github.com/mptooling/notifycat/internal/notification/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/bitbucket"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/github"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/security"
	"github.com/mptooling/notifycat/internal/platform/slack"
	reviewapp "github.com/mptooling/notifycat/internal/review/application"
	reviewdomain "github.com/mptooling/notifycat/internal/review/domain"
	reviewinfra "github.com/mptooling/notifycat/internal/review/infrastructure"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	validationapp "github.com/mptooling/notifycat/internal/validation/application"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
	validationinfra "github.com/mptooling/notifycat/internal/validation/infrastructure"
)

// Module is the runtime composition root. Supply a config.Config and it builds
// the whole graph — logger, mappings provider, migrated database, Slack client,
// per-PR router, dispatcher, start-review sink, HTTP server, and the cleanup and
// digest schedulers — then starts the startup-validation gate, the HTTP server,
// and the schedulers via lifecycle hooks. A failing startup gate or a bad cron
// spec fails App.Start; the database handle closes on App.Stop.
var Module = fx.Module("runtime",
	fx.Provide(
		newLogger,
		newHTTPClient,
		buildSlackClient,
		newComposer,
		openAndMigrate,
		persistence.NewPullRequests,
		persistence.NewCodeReviews,
		buildProvider,
		buildRouter,
		buildDispatcher,
		buildStartReviewSink,
		buildCleanupScheduler,
		buildDigestScheduler,
		buildMux,
		buildServer,
		newRunContext,
	),
	fx.Invoke(startupGate),
	fx.Invoke(registerServer),
	fx.Invoke(registerSchedulers),
)

// newHTTPClient builds the shared HTTP client used for Slack, GitHub, and
// startup validation, with the same 10s timeout Wire used.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// newComposer builds the Slack message composer from the configured new-PR
// reaction.
func newComposer(cfg config.Config) *slack.Composer {
	return slack.NewComposer(cfg.Reactions.NewPR)
}

// buildProvider constructs the mappings provider from config and warns when
// path routing is configured but no GitHub token is available to read a PR's
// changed files — path rules are then inert and PRs route to the repo tier.
func buildProvider(cfg config.Config, logger *slog.Logger) *routingapp.Provider {
	defaults := routingdomain.Defaults{
		Reactions: persistence.Reactions{
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
		GitProvider:      cfg.GitProvider,
	}
	provider := routingapp.NewProvider(defaults, cfg.Mappings, cfg.Digest)
	if provider.HasPathRules() && cfg.ProviderToken().Reveal() == "" {
		logger.Warn(fmt.Sprintf("path routing is configured but %s is unset; "+
			"path rules are inert and PRs route to the repo tier (a token is needed to read a PR's changed files)",
			cfg.ProviderTokenVar()))
	}
	return provider
}

// openAndMigrate opens the database and applies pending migrations. On any
// failure it releases the handle, so the caller never holds an unclosed db. The
// handle is closed via the fx.Lifecycle OnStop hook registered here.
func openAndMigrate(lc fx.Lifecycle, cfg config.Config) (*gorm.DB, error) {
	db, err := persistence.Open(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := persistence.MigrateUp(context.Background(), db); err != nil {
		closeDB(db)
		return nil, fmt.Errorf("app: migrate: %w", err)
	}
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			closeDB(db)
			return nil
		},
	})
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
func buildCleanupScheduler(cfg config.Config, pullRequests *persistence.PullRequests, logger *slog.Logger) *maintenanceapp.Cleaner {
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
// fails startup here rather than silently never firing. Because this provider
// returns an error, fx fails App.Start on a bad spec.
func buildDigestScheduler(cfg config.Config, provider *routingapp.Provider, pullRequests *persistence.PullRequests, slackClient *slack.Client, composer *slack.Composer, logger *slog.Logger) (*digestapp.Scheduler, error) {
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
	return routingapp.NewRouter(provider, providerFilesFetcher(httpClient, cfg), logger)
}

// providerFilesFetcher builds the changed-files reader for the configured git
// provider, or nil when that provider's read token is unset (path rules then go
// inert — identical degradation for github and bitbucket).
func providerFilesFetcher(httpClient *http.Client, cfg config.Config) routingdomain.ChangedFilesReader {
	switch cfg.GitProvider {
	case kernel.ProviderBitbucket:
		if cfg.BitbucketToken.Reveal() == "" {
			return nil
		}
		return bitbucket.NewClient(httpClient, cfg.BitbucketToken.Reveal(), cfg.BitbucketAuthEmail, bitbucket.WithBaseURL(cfg.BitbucketBaseURL))
	default:
		if cfg.GitHubToken.Reveal() == "" {
			return nil
		}
		return github.NewClient(httpClient, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
}

// buildDispatcher wires the PR-event handlers behind the dispatcher.
func buildDispatcher(pullRequests *persistence.PullRequests, codeReviews *persistence.CodeReviews, provider *routingapp.Provider, router *routingapp.Router, slackClient *slack.Client, composer *slack.Composer, logger *slog.Logger) *notificationapp.Dispatcher {
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

// buildStartReviewSink wires the start-review use case behind the Slack
// interaction sink used by the inbound interactivity endpoint.
func buildStartReviewSink(pullRequests *persistence.PullRequests, codeReviews *persistence.CodeReviews, slackClient *slack.Client, composer *slack.Composer, logger *slog.Logger) reviewinfra.InteractionSink {
	startReview := reviewapp.NewHandler(reviewdomain.HandlerParams{
		Recorder:  reviewinfra.NewCodeReviewsRepo(codeReviews),
		Messages:  reviewinfra.NewMessageChecker(pullRequests),
		Decorator: reviewinfra.NewSlackDecorator(composer, slackClient),
		Logger:    logger,
		Now:       time.Now,
	})
	return reviewinfra.NewStartReviewSink(startReview, logger)
}

// buildMux builds the HTTP routes: health check, the selected git provider's
// webhook, and the optional inbound Slack interactivity endpoint. The Slack
// route is registered only when a signing secret is configured; otherwise
// notifycat stays outbound-only and the route is absent.
func buildMux(cfg config.Config, dispatcher *notificationapp.Dispatcher, startReviewSink reviewinfra.InteractionSink, logger *slog.Logger) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	registerWebhookRoute(mux, cfg, dispatcher)

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

// registerWebhookRoute registers the inbound webhook route for the configured git
// provider — only the selected provider's route, verifier, and handler exist, so
// a github deployment has no /webhook/bitbucket and vice versa. Both verifiers
// reject an unsigned or mis-signed delivery with 401.
func registerWebhookRoute(mux *http.ServeMux, cfg config.Config, dispatcher *notificationapp.Dispatcher) {
	switch cfg.GitProvider {
	case kernel.ProviderBitbucket:
		verifier := security.NewBitbucketVerifier(cfg.BitbucketWebhookSecret.Reveal())
		mux.Handle("POST /webhook/bitbucket",
			notificationinfra.BitbucketSignatureMiddleware(verifier)(
				notificationinfra.NewBitbucketHandler(dispatcher),
			),
		)
	default:
		verifier := security.NewGitHubVerifier(cfg.GitHubWebhookSecret.Reveal())
		mux.Handle("POST /webhook/github",
			notificationinfra.SignatureMiddleware(verifier)(
				notificationinfra.NewGitHubHandler(dispatcher),
			),
		)
	}
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

// runContext carries the cancellable context the schedulers run under. It is a
// module-owned value so the OnStart hook can launch the scheduler goroutines and
// the OnStop hook can cancel them.
type runContext struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newRunContext() *runContext {
	ctx, cancel := context.WithCancel(context.Background())
	return &runContext{ctx: ctx, cancel: cancel}
}

// startupGate runs the cache-aware validation pipeline before the server begins
// serving. Because a failing fx.Invoke aborts App.Start before any OnStart hook
// runs, a returned error naturally gates the server (fail-fast preserved).
func startupGate(provider *routingapp.Provider, cfg config.Config, slackClient *slack.Client, httpClient *http.Client, logger *slog.Logger) error {
	return startupValidate(provider, cfg, slackClient, httpClient, logger)
}

// registerServer wires the HTTP server into the lifecycle: OnStart launches
// ListenAndServe in a goroutine (ignoring http.ErrServerClosed), OnStop calls
// Shutdown. A fatal ListenAndServe error (e.g. the address is already in use)
// triggers a shutdown with exit code 1 via the Shutdowner, preserving the legacy
// entrypoint's fatal-server-error → non-zero-exit behaviour.
func registerServer(lc fx.Lifecycle, server *http.Server, logger *slog.Logger, shutdowner fx.Shutdowner) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				logger.Info("listening", slog.String("addr", server.Addr))
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("http server stopped", slog.Any("err", err))
					_ = shutdowner.Shutdown(fx.ExitCode(1))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return server.Shutdown(ctx)
		},
	})
}

// registerSchedulers wires the cleanup and digest schedulers into the
// lifecycle: OnStart launches each Run under the shared cancellable runCtx,
// OnStop cancels it. The digest scheduler is nil when no mapping enables a
// digest, and is then skipped.
func registerSchedulers(lc fx.Lifecycle, runCtx *runContext, cleaner *maintenanceapp.Cleaner, digestScheduler *digestapp.Scheduler) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() { _ = cleaner.Run(runCtx.ctx) }()
			if digestScheduler != nil {
				go func() { _ = digestScheduler.Run(runCtx.ctx) }()
			}
			return nil
		},
		OnStop: func(context.Context) error {
			runCtx.cancel()
			return nil
		},
	})
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

func newValidationDeps(provider *routingapp.Provider, cfg config.Config, slackClient *slack.Client, httpClient *http.Client) (validationdomain.RepoValidator, validationdomain.RepoLister) {
	hook, lister := providerValidationDeps(httpClient, cfg)
	return validationapp.NewValidator(provider, validationinfra.NewSlackProbe(slackClient), hook), lister
}

// providerValidationDeps builds the selected provider's webhook-coverage probe
// and repo lister. When the provider's read token is unset the probe's Checker
// and the lister are nil, so validation skips the hook/wildcard checks — the same
// degradation for github and bitbucket.
func providerValidationDeps(httpClient *http.Client, cfg config.Config) (validationdomain.HookProbe, validationdomain.RepoLister) {
	switch cfg.GitProvider {
	case kernel.ProviderBitbucket:
		hook := validationdomain.HookProbe{URLSuffix: validationdomain.WebhookURLPathBitbucket, RequiredEvents: validationdomain.RequiredBitbucketEvents}
		if cfg.BitbucketToken.Reveal() == "" {
			return hook, nil
		}
		client := bitbucket.NewClient(httpClient, cfg.BitbucketToken.Reveal(), cfg.BitbucketAuthEmail, bitbucket.WithBaseURL(cfg.BitbucketBaseURL))
		hook.Checker = client
		return hook, validationinfra.NewBitbucketRepoLister(client)
	default:
		hook := validationdomain.HookProbe{URLSuffix: validationdomain.WebhookURLPathGitHub, RequiredEvents: validationdomain.RequiredGitHubEvents}
		if cfg.GitHubToken.Reveal() == "" {
			return hook, nil
		}
		client := github.NewClient(httpClient, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
		hook.Checker = client
		return hook, client
	}
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
	if sqlDB, err := persistence.SQLDB(db); err == nil {
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
