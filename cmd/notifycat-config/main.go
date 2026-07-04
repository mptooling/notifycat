// Command notifycat-config is the CLI for the declarative config.yaml
// workflow: `list` prints the file, `validate` runs the cache-aware
// validation pipeline. This file owns argument parsing and dispatches to
// use cases in internal/mappingcli.
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
	"github.com/mptooling/notifycat/internal/github"
	"github.com/mptooling/notifycat/internal/mappingcli"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/validate"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-config:", err)
		os.Exit(1)
	}
	provider := routingapp.NewProvider(routingdomain.Defaults{}, cfg.Mappings, cfg.Digest)
	if w := pathTokenWarning(provider, cfg.GitHubToken.Reveal() != ""); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}
	checker, lister := buildValidationDeps(cfg, provider)
	validator := mappingcli.NewMappingsValidator(
		provider,
		checker,
		lister,
		routinginfra.LockPath(cfg.ConfigFile),
		time.Now,
	)
	os.Exit(dispatch(os.Args[1:], provider, validator, os.Stdout, os.Stderr))
}

// buildValidationDeps wires the production checker (Slack always, GitHub
// when a token is configured) and the org-repo lister (the same GitHub
// client, or nil when there is no token).
func buildValidationDeps(cfg config.Config, provider *routingapp.Provider) (validate.RepoValidator, validate.OrgRepoLister) {
	hc := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	var gh *github.Client
	if cfg.GitHubToken.Reveal() != "" {
		gh = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	var ghChecker validate.GitHubChecker
	var lister validate.OrgRepoLister
	if gh != nil {
		ghChecker = gh
		lister = gh
	}
	return validate.NewValidator(provider, slackClient, ghChecker), lister
}

// pathTokenWarning returns a warning when per-path routing is configured but no
// GitHub token is available — path rules are inert without one (a token is
// needed to read a PR's changed files). It returns "" when there is nothing to
// warn about. Kept separate from main so it can be unit-tested.
func pathTokenWarning(provider *routingapp.Provider, hasGitHubToken bool) string {
	if provider.HasPathRules() && !hasGitHubToken {
		return "warning: path routing is configured but GITHUB_TOKEN is unset; " +
			"path rules are inert and PRs route to the repo tier until a token is set"
	}
	return ""
}

func dispatch(args []string, provider *routingapp.Provider, validator mappingcli.MappingsValidator, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "list":
		return mappingcli.List(provider, stdout)
	case "validate":
		return runValidate(ctx, args[1:], validator, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage())
		return 2
	}
}

func runValidate(ctx context.Context, args []string, validator mappingcli.MappingsValidator, stdout, stderr io.Writer) int {
	target, force, code, ok := parseValidateArgs(args, stderr)
	if !ok {
		return code
	}
	return validator.Validate(ctx, target, force, stdout, stderr)
}

func parseValidateArgs(args []string, stderr io.Writer) (target string, force bool, code int, ok bool) {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: validate [owner/repo] [--force]") }
	forcePtr := fs.Bool("force", false, "ignore the lock file; full revalidation")
	if err := fs.Parse(args); err != nil {
		return "", false, 2, false
	}
	positional := fs.Args()
	switch len(positional) {
	case 0:
		return "", *forcePtr, 0, true
	case 1:
		return positional[0], *forcePtr, 0, true
	default:
		fs.Usage()
		return "", false, 2, false
	}
}

func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-config list
  notifycat-config validate [owner/repo] [--force]
`)
}
