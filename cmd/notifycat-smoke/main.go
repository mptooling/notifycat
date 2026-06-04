// Command notifycat-smoke runs an end-to-end delivery test against the running
// notifycat stack. It forges a signed `pull_request: opened` webhook for a
// mapped repository, POSTs it to the live /webhook/github endpoint, and reports
// the Slack channel and message timestamp it produced — the honest "does my
// config actually deliver?" check to run before wiring a real GitHub webhook.
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
	target, url, code, ok := parseArgs(args, stderr)
	if !ok {
		return code
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "notifycat-smoke: config load failed:", err)
		fmt.Fprintln(stderr, "see docs/configuration.md for required environment variables")
		return 1
	}

	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		fmt.Fprintln(stderr, "notifycat-smoke: cannot load mappings:", err)
		return 1
	}
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(stderr, "notifycat-smoke: cannot open database:", err)
		return 1
	}
	messages := store.NewSlackMessages(db)

	hc := &http.Client{Timeout: 15 * time.Second}
	s := smoke.New(provider, messages, hc, cfg.GitHubWebhookSecret.Reveal(), url, time.Now)

	res, err := s.Run(context.Background(), target)
	if err != nil {
		return report(stderr, target, url, err)
	}

	fmt.Fprintf(stdout, "✓ delivered a smoke test to %s\n", res.Repository)
	fmt.Fprintf(stdout, "  channel:    %s\n", res.Channel)
	fmt.Fprintf(stdout, "  timestamp:  %s\n", res.Timestamp)
	fmt.Fprintf(stdout, "  title:      %s\n", res.Title)
	fmt.Fprintln(stdout, "A real Slack message was posted — delete it from the channel when you're done.")
	return 0
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

func parseArgs(args []string, stderr io.Writer) (target, url string, code int, ok bool) {
	fs := flag.NewFlagSet("notifycat-smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage()) }
	urlFlag := fs.String("url", defaultURL, "webhook endpoint to POST the synthetic event to")
	if err := fs.Parse(args); err != nil {
		return "", "", 2, false
	}
	positional := fs.Args()
	if len(positional) != 1 {
		fs.Usage()
		return "", "", 2, false
	}
	return positional[0], *urlFlag, 0, true
}

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-smoke owner/repo           # post a signed test event to the running server
  notifycat-smoke --url URL owner/repo # override the endpoint (default: ` + defaultURL + `)
`)
}
