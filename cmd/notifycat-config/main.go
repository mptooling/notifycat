// Command notifycat-config is the CLI for the declarative config.yaml
// workflow: `list` prints the file, `validate` runs the cache-aware
// validation pipeline.
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
	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/platform/bitbucket"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/github"
	"github.com/mptooling/notifycat/internal/platform/slack"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	routinginfra "github.com/mptooling/notifycat/internal/routing/infrastructure"
	validationapp "github.com/mptooling/notifycat/internal/validation/application"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
	validationinfra "github.com/mptooling/notifycat/internal/validation/infrastructure"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-config:", err)
		os.Exit(1)
	}
	provider := routingapp.NewProvider(routingdomain.Defaults{GitProvider: cfg.GitProvider}, cfg.Mappings, cfg.Digest)
	if w := pathTokenWarning(provider, cfg); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}
	checker, lister := buildValidationDeps(cfg, provider)
	lockPath := routinginfra.LockPath(cfg.ConfigFile)
	gateway := diagnosticsinfra.NewLockGateway(lockPath, time.Now)
	validator := diagnosticsapp.NewMappingsValidator(provider, checker, lister, gateway)
	os.Exit(dispatch(os.Args[1:], provider, validator, os.Stdout, os.Stderr))
}

// buildValidationDeps wires the production checker (Slack always, plus the
// selected git provider's webhook-coverage probe when a token is configured) and
// the repo lister for wildcard expansion (nil when there is no token).
func buildValidationDeps(cfg config.Config, provider *routingapp.Provider) (validationdomain.RepoValidator, validationdomain.RepoLister) {
	hc := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	hook, lister := providerValidationDeps(hc, cfg)
	return validationapp.NewValidator(provider, validationinfra.NewSlackProbe(slackClient), hook), lister
}

// providerValidationDeps builds the selected provider's webhook-coverage probe
// and repo lister; both are nil-checker/nil when the provider's read token is
// unset, so validation skips the hook and wildcard checks (identical degradation
// for github and bitbucket).
func providerValidationDeps(hc *http.Client, cfg config.Config) (validationdomain.HookProbe, validationdomain.RepoLister) {
	switch cfg.GitProvider {
	case kernel.ProviderBitbucket:
		hook := validationdomain.HookProbe{URLSuffix: validationdomain.WebhookURLPathBitbucket, RequiredEvents: validationdomain.RequiredBitbucketEvents}
		if cfg.BitbucketToken.Reveal() == "" {
			return hook, nil
		}
		client := bitbucket.NewClient(hc, cfg.BitbucketToken.Reveal(), cfg.BitbucketAuthEmail, bitbucket.WithBaseURL(cfg.BitbucketBaseURL))
		hook.Checker = client
		return hook, validationinfra.NewBitbucketRepoLister(client)
	default:
		hook := validationdomain.HookProbe{URLSuffix: validationdomain.WebhookURLPathGitHub, RequiredEvents: validationdomain.RequiredGitHubEvents}
		if cfg.GitHubToken.Reveal() == "" {
			return hook, nil
		}
		client := github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
		hook.Checker = client
		return hook, client
	}
}

// pathTokenWarning returns a warning when per-path routing is configured but the
// selected provider's read token is unset — path rules are inert without one (a
// token is needed to read a PR's changed files). It returns "" when there is
// nothing to warn about.
func pathTokenWarning(provider *routingapp.Provider, cfg config.Config) string {
	if provider.HasPathRules() && cfg.ProviderToken().Reveal() == "" {
		return fmt.Sprintf("warning: path routing is configured but %s is unset; "+
			"path rules are inert and PRs route to the repo tier until a token is set", cfg.ProviderTokenVar())
	}
	return ""
}

// mappingsValidator is the interface the dispatch + runValidate functions use.
// *diagnosticsapp.MappingsValidator satisfies it in production; the test file
// provides a fake.
type mappingsValidator interface {
	Validate(ctx context.Context, target string, force bool, stdout, stderr io.Writer) int
}

func dispatch(args []string, provider *routingapp.Provider, validator mappingsValidator, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	ctx := context.Background()
	switch args[0] {
	case "list":
		return diagnosticsapp.List(provider, stdout)
	case "validate":
		return runValidate(ctx, args[1:], validator, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s\n", args[0], usage())
		return 2
	}
}

func runValidate(ctx context.Context, args []string, validator mappingsValidator, stdout, stderr io.Writer) int {
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
