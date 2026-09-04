// Command notifycat-relocate moves the Slack messages of open PRs from one
// channel to another after an operator repoints a repository in config.yaml.
//
// Changing a repo's `channel:` only redirects *new* PRs: the messages of PRs
// that were already open keep living in the old channel, because the database
// records where each message was posted. This tool carries them over — it
// reposts each message in the new channel as a mention-free "moved" notice,
// retargets the stored row, carries the repo's own reactions across, and
// deletes the original.
//
// Usage:
//
//	notifycat-relocate -audit                        — list messages sitting in channels config no longer mentions
//	notifycat-relocate -from C_OLD -to C_NEW          — move every open PR's message
//	notifycat-relocate -from C_OLD -to C_NEW -repo org/repo
//	notifycat-relocate -from C_OLD                    — delete the messages, no replacement
//	notifycat-relocate -from C_OLD -to C_NEW -dry-run — report what would change, write nothing
//
// A PR that already has a message in the destination keeps that one and only
// loses the original, so a re-run never duplicates a message. The run is
// idempotent: the stored row is retargeted immediately after the repost, so
// anything already moved is skipped next time.
//
// SLACK_BOT_TOKEN needs chat:write, reactions:write, reactions:read and the
// history scopes (channels:history, groups:history) — reading a message to
// repost it is the one thing the server itself never does. DATABASE_URL must
// point at the same database the server uses.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	maintenanceapp "github.com/mptooling/notifycat/internal/maintenance/application"
	maintenancedomain "github.com/mptooling/notifycat/internal/maintenance/domain"
	maintenanceinfra "github.com/mptooling/notifycat/internal/maintenance/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// requiredScopes are the Slack scopes a relocate run calls: the server's own
// two plus the reads only this tool performs. They are checked here rather than
// added to the server's required set, so an operator who never relocates does
// not have to widen the app's permissions.
var requiredScopes = []string{
	"chat:write",
	"reactions:write",
	"reactions:read",
	"channels:history",
	"groups:history",
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-relocate:", err)
		os.Exit(1)
	}
}

type options struct {
	from       string
	to         string
	repository string
	dryRun     bool
	audit      bool
}

func run(args []string) error {
	opts, err := parseOptions(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	db, err := persistence.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() {
		if sqlDB, err := persistence.SQLDB(db); err == nil {
			_ = sqlDB.Close()
		}
	}()

	httpClient := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(httpClient, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	provider := routingapp.NewProvider(routingDefaults(cfg), cfg.Mappings, cfg.Digest)
	repository := maintenanceinfra.NewPRRepository(persistence.NewPullRequests(db))
	policy := maintenanceinfra.NewRoutingPolicy(provider)

	relocator := maintenanceapp.NewRelocator(maintenancedomain.RelocatorParams{
		Lister:     repository,
		Rows:       repository,
		Courier:    maintenanceinfra.NewSlackCourier(slackClient),
		Reactions:  policy,
		Channels:   policy,
		Logger:     logger,
		From:       opts.from,
		To:         opts.to,
		Repository: opts.repository,
		DryRun:     opts.dryRun,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if opts.audit {
		return audit(ctx, relocator)
	}
	if err := preflight(ctx, slackClient, opts); err != nil {
		return err
	}
	return relocate(ctx, relocator, opts)
}

// routingDefaults mirrors the server's global tier so a relocated message
// carries the same reaction set the handlers would have added.
func routingDefaults(cfg config.Config) routingdomain.Defaults {
	return routingdomain.Defaults{
		Reactions: routingdomain.Reactions{
			Enabled:       cfg.Reactions.Enabled,
			NewPR:         cfg.Reactions.NewPR,
			MergedPR:      cfg.Reactions.MergedPR,
			ClosedPR:      cfg.Reactions.ClosedPR,
			Approved:      cfg.Reactions.Approved,
			Commented:     cfg.Reactions.Commented,
			RequestChange: cfg.Reactions.RequestChange,
			BotReview:     cfg.Reactions.BotReview,
		},
		GitProvider: cfg.GitProvider,
	}
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("notifycat-relocate", flag.ContinueOnError)
	var opts options
	fs.StringVar(&opts.from, "from", "", "Slack channel id to move messages out of")
	fs.StringVar(&opts.to, "to", "", "Slack channel id to move messages into; empty deletes them instead")
	fs.StringVar(&opts.repository, "repo", "", `narrow the run to one "org/repo"`)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "report what would change without writing")
	fs.BoolVar(&opts.audit, "audit", false, "list messages in channels the config no longer mentions, then exit")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	if opts.audit {
		return opts, nil
	}
	if opts.from == "" {
		return options{}, fmt.Errorf("-from is required (or -audit to see what needs moving)")
	}
	if !routingdomain.IsChannelID(opts.from) {
		return options{}, fmt.Errorf("-from %q is not a Slack channel id", opts.from)
	}
	if opts.to != "" && !routingdomain.IsChannelID(opts.to) {
		return options{}, fmt.Errorf("-to %q is not a Slack channel id", opts.to)
	}
	if opts.to == opts.from {
		return options{}, fmt.Errorf("-from and -to are the same channel")
	}
	return opts, nil
}

// preflight refuses a run whose writes would fail partway: a token missing a
// scope, or a destination the bot cannot post to.
func preflight(ctx context.Context, client *slack.Client, opts options) error {
	_, scopes, err := client.AuthTest(ctx)
	if err != nil {
		return fmt.Errorf("slack token check: %w", err)
	}
	if missing := missingScopes(scopes, requiredScopes); len(missing) > 0 {
		return fmt.Errorf("SLACK_BOT_TOKEN is missing scope(s) %v; reinstall the app with them granted", missing)
	}
	if opts.to == "" {
		return nil
	}
	info, err := client.ConversationsInfo(ctx, opts.to)
	if err != nil {
		return fmt.Errorf("destination channel %s: %w", opts.to, err)
	}
	if info.IsArchived {
		return fmt.Errorf("destination channel %s is archived", opts.to)
	}
	if !info.IsMember {
		return fmt.Errorf("the bot is not a member of destination channel %s; invite it first", opts.to)
	}
	return nil
}

// missingScopes returns the required scopes absent from granted.
func missingScopes(granted, required []string) []string {
	have := make(map[string]bool, len(granted))
	for _, scope := range granted {
		have[scope] = true
	}
	var missing []string
	for _, scope := range required {
		if !have[scope] {
			missing = append(missing, scope)
		}
	}
	return missing
}

func audit(ctx context.Context, relocator *maintenanceapp.Relocator) error {
	stale, err := relocator.Audit(ctx)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		fmt.Println("audit: every tracked message sits in a channel the config still routes to")
		return nil
	}
	fmt.Printf("audit: %d message(s) in channels the config no longer mentions\n", len(stale))
	for _, message := range stale {
		fmt.Printf("  %s#%d  %s\n", message.Repository, message.PRNumber, message.Channel)
	}
	fmt.Println("move them with: notifycat-relocate -from <channel> -to <channel>")
	return nil
}

func relocate(ctx context.Context, relocator *maintenanceapp.Relocator, opts options) error {
	summary, err := relocator.Run(ctx)
	if err != nil {
		return err
	}

	mode := "applied"
	if opts.dryRun {
		mode = "dry-run"
	}
	fmt.Printf("relocate (%s): scanned=%d moved=%d merged=%d dropped=%d errors=%d\n",
		mode, summary.Scanned, summary.Moved, summary.Merged, summary.Dropped, summary.Errors)
	if summary.Errors > 0 {
		return fmt.Errorf("%d message(s) could not be relocated; resolve the logged cause and re-run — it is idempotent", summary.Errors)
	}
	return nil
}
