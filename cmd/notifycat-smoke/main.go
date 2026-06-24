// Command notifycat-smoke runs an end-to-end delivery test against the running
// notifycat stack. It forges a signed `pull_request: opened` webhook for a
// mapped repository, POSTs it to the live /webhook/github endpoint, and reports
// the Slack channel and message timestamp it produced — the honest "does my
// config actually deliver?" check to run before wiring a real GitHub webhook.
//
// With --reactions it also replays a comment, an approval, and a merge for the
// same synthetic PR and verifies the configured emoji landed on the message.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappings"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/smoke"
	"github.com/mptooling/notifycat/internal/store"
)

// defaultURL targets the server over the compose network — the same name the
// doctor one-off container reaches. Override with --url for a host-local run.
const defaultURL = "http://notifycat:8080/webhook/github"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, code, ok := parseArgs(args, stderr)
	if !ok {
		return code
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "notifycat-smoke: config load failed:", err)
		fmt.Fprintln(stderr, "see docs/configuration.md for required environment variables")
		return 1
	}

	provider := mappings.NewProvider(cfg.Mappings, cfg.Digest)
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(stderr, "notifycat-smoke: cannot open database:", err)
		return 1
	}
	messages := store.NewSlackMessages(db)

	hc := &http.Client{Timeout: 15 * time.Second}
	slackClient := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	s := smoke.New(provider, messages, slackClient, hc, cfg.GitHubWebhookSecret.Reveal(), opts.url, cfg.Reactions, cfg.IgnoreAIReviews, time.Now)

	res, err := s.Run(context.Background(), opts.target, opts.reactions)
	if err != nil {
		return report(stderr, opts.target, opts.url, err)
	}
	return render(stdout, res)
}

// render prints a successful result and returns the exit code — non-zero when a
// requested reaction was expected but did not appear on the message.
func render(stdout io.Writer, res smoke.Result) int {
	fmt.Fprintf(stdout, "✓ delivered a smoke test to %s\n", res.Repository)
	fmt.Fprintf(stdout, "  channel:    %s\n", res.Channel)
	fmt.Fprintf(stdout, "  timestamp:  %s\n", res.Timestamp)
	fmt.Fprintf(stdout, "  title:      %s\n", res.Title)

	exit := 0
	if res.ReactionsRequested {
		exit = renderReactions(stdout, res)
	}

	fmt.Fprintln(stdout, "A real Slack message was posted — delete it from the channel when you're done.")
	return exit
}

// renderReactions prints the reaction-lifecycle section and returns 1 if any
// requested emoji was confirmed absent. A verify failure (couldn't read the
// reactions back) is surfaced but not treated as a smoke failure.
func renderReactions(stdout io.Writer, res smoke.Result) int {
	if !res.ReactionsEnabled {
		fmt.Fprintln(stdout, "  reactions:  disabled in config (SLACK_REACTIONS_ENABLED=false) — skipped")
		return 0
	}

	fmt.Fprintln(stdout, "  reactions:")
	exit := 0
	for _, c := range res.Reactions {
		switch {
		case c.VerifyErr != nil:
			fmt.Fprintf(stdout, "    ?  %-8s %-26s could not verify: %v\n", c.Step, c.Emoji, c.VerifyErr)
		case c.Present:
			fmt.Fprintf(stdout, "    ✓  %-8s %s\n", c.Step, c.Emoji)
		default:
			fmt.Fprintf(stdout, "    ✗  %-8s %-26s not found on the message\n", c.Step, c.Emoji)
			exit = 1
		}
	}

	// Make a skipped bot-review step explicit — silence here would read as
	// "covered" when it wasn't.
	switch {
	case res.IgnoreAIReviews:
		fmt.Fprintln(stdout, "    –  bot      skipped (NOTIFYCAT_IGNORE_AI_REVIEWS=true — bot reviews are muted)")
	case res.BotReviewMarker == "":
		fmt.Fprintln(stdout, "    –  bot      skipped (SLACK_REACTION_BOT_REVIEW is empty — no bot marker configured)")
	}
	return exit
}

// report maps a Smoke error to a clear, stack-trace-free message and exit code.
func report(stderr io.Writer, target, url string, err error) int {
	switch {
	case errors.Is(err, smoke.ErrNoMapping):
		fmt.Fprintf(stderr, "notifycat-smoke: %s is not in mappings.yaml — add it before smoke-testing\n", target)
	case errors.Is(err, smoke.ErrSignatureRejected):
		fmt.Fprintln(stderr, "notifycat-smoke: the server rejected the signature (401).")
		fmt.Fprintln(stderr, "  the GITHUB_WEBHOOK_SECRET this command used does not match the running server's — check your .env")
	case errors.Is(err, smoke.ErrUnreachable):
		fmt.Fprintf(stderr, "notifycat-smoke: could not reach the server at %s — is the stack running? (docker compose up -d)\n", url)
	default:
		fmt.Fprintln(stderr, "notifycat-smoke:", err)
	}
	return 1
}

type options struct {
	target    string
	url       string
	reactions bool
}

func parseArgs(args []string, stderr io.Writer) (opts options, code int, ok bool) {
	fs := flag.NewFlagSet("notifycat-smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage()) }
	url := fs.String("url", defaultURL, "webhook endpoint to POST the synthetic event to")
	reactions := fs.Bool("reactions", false, "also replay comment/approve/merge and verify the configured emoji")
	if err := fs.Parse(args); err != nil {
		return options{}, 2, false
	}
	positional := fs.Args()
	if len(positional) != 1 {
		fs.Usage()
		return options{}, 2, false
	}
	return options{target: positional[0], url: *url, reactions: *reactions}, 0, true
}

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-smoke owner/repo              # post a signed test event to the running server
  notifycat-smoke --reactions owner/repo  # also replay comment/approve/merge and verify emoji
  notifycat-smoke --url URL owner/repo    # override the endpoint (default: ` + defaultURL + `)
`)
}
