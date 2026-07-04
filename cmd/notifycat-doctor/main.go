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

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/doctor"
	"github.com/mptooling/notifycat/internal/github"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/validate"
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

	validator := buildValidator(cfg)

	d := doctor.NewDoctor(cfg, validator)
	sections := d.Run(context.Background(), target)
	if !doctor.WriteReport(stdout, sections) {
		return 1
	}
	return 0
}

// buildValidator constructs a RepoValidator from in-memory config.
func buildValidator(cfg config.Config) doctor.RepoValidator {
	provider := routingapp.NewProvider(routingdomain.Defaults{}, cfg.Mappings, cfg.Digest)
	hc := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	var ghChecker validate.GitHubChecker
	if cfg.GitHubToken.Reveal() != "" {
		ghChecker = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	return validate.NewValidator(provider, slackClient, ghChecker)
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
