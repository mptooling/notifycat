// Command notifycat-doctor runs end-to-end preflight diagnostics for a
// notifycat installation. With no argument it checks config + database +
// mappings file. With `owner/repo` it additionally delegates to the
// validate package for Slack + GitHub webhook checks on that repository.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	diagnosticsapp "github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsinfra "github.com/mptooling/notifycat/internal/diagnostics/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/github"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	validationapp "github.com/mptooling/notifycat/internal/validation/application"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
	validationinfra "github.com/mptooling/notifycat/internal/validation/infrastructure"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	target, code, ok := parseArgs(args, stderr)
	if !ok {
		return code
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "notifycat-doctor: config load failed:", err)
		fmt.Fprintln(stderr, "see docs/configuration.md for required environment variables")
		return 1
	}

	provider := routingapp.NewProvider(routingdomain.Defaults{GitProvider: cfg.GitProvider}, cfg.Mappings, cfg.Digest)
	entries := provider.Entries()
	hasPathRules := provider.HasPathRules()

	snapshot := diagnosticsinfra.NewConfigSnapshot(cfg, entries, hasPathRules)
	validator := buildValidator(cfg, provider)

	doctor := diagnosticsapp.NewDoctor(snapshot, validator)
	sections := doctor.Run(context.Background(), target)
	if !diagnosticsinfra.WriteReport(stdout, sections) {
		return 1
	}
	return 0
}

// buildValidator constructs a RepoValidator from in-memory config.
func buildValidator(cfg config.Config, provider *routingapp.Provider) validationdomain.RepoValidator {
	hc := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	var ghChecker validationdomain.GitHubChecker
	if cfg.GitHubToken.Reveal() != "" {
		ghChecker = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	return validationapp.NewValidator(provider, validationinfra.NewSlackProbe(slackClient), ghChecker)
}

func parseArgs(args []string, stderr io.Writer) (target string, code int, ok bool) {
	fs := flag.NewFlagSet("notifycat-doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, usage()) }
	if err := fs.Parse(args); err != nil {
		return "", 2, false
	}
	positional := fs.Args()
	switch len(positional) {
	case 0:
		return "", 0, true
	case 1:
		return positional[0], 0, true
	default:
		fs.Usage()
		return "", 2, false
	}
}

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-doctor                # preflight (config + database + mappings)
  notifycat-doctor owner/repo     # preflight + Slack + GitHub webhook checks for one repo
`)
}
