// Package config loads runtime configuration from environment variables (and
// optionally from a .env file in development).
//
// All secret values use the Secret type so they cannot leak through logging.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the parsed runtime configuration.
type Config struct {
	Addr        string `env:"ADDR" envDefault:":8080"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat   string `env:"LOG_FORMAT" envDefault:"text"`
	DatabaseURL string `env:"DATABASE_URL" envDefault:"file:./data/notifycat.db"`

	GitHubWebhookSecret Secret `env:"GITHUB_WEBHOOK_SECRET,required,notEmpty"`
	SlackBotToken       Secret `env:"SLACK_BOT_TOKEN,required,notEmpty"`

	// SlackBaseURL is operator-overridable to point the client at a test
	// double. Defaults to the real Slack API.
	SlackBaseURL string `env:"SLACK_BASE_URL" envDefault:"https://slack.com"`

	// GitHubToken is consumed only by `notifycat-mapping validate` to query the
	// repository's webhook configuration. Optional; without it the webhook
	// coverage check is skipped.
	GitHubToken Secret `env:"GITHUB_TOKEN"`

	// GitHubBaseURL is operator-overridable, paralleling SlackBaseURL.
	GitHubBaseURL string `env:"GITHUB_BASE_URL" envDefault:"https://api.github.com"`

	Reactions Reactions
}

// Reactions configures Slack reaction emoji names per PR lifecycle event.
type Reactions struct {
	Enabled       bool   `env:"SLACK_REACTIONS_ENABLED" envDefault:"true"`
	NewPR         string `env:"SLACK_REACTION_NEW_PR" envDefault:"large_green_circle"`
	MergedPR      string `env:"SLACK_REACTION_MERGED_PR" envDefault:"twisted_rightwards_arrows"`
	ClosedPR      string `env:"SLACK_REACTION_CLOSED_PR" envDefault:"x"`
	Approved      string `env:"SLACK_REACTION_PR_APPROVED" envDefault:"white_check_mark"`
	Commented     string `env:"SLACK_REACTION_PR_COMMENTED" envDefault:"speech_balloon"`
	RequestChange string `env:"SLACK_REACTION_PR_REQUEST_CHANGE" envDefault:"exclamation"`
}

// MissingVarError is returned by Load when a required env var is unset or empty.
type MissingVarError struct {
	Var string
}

func (e *MissingVarError) Error() string {
	return fmt.Sprintf("config: required environment variable %s is missing", e.Var)
}

// Load reads .env if present (no-op when absent) and parses environment
// variables into Config.
func Load() (Config, error) {
	// Best-effort dev convenience: a missing .env is fine.
	_ = godotenv.Load()

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, translateParseError(err)
	}
	return cfg, nil
}

// translateParseError converts caarlos0/env's "required" / "not-empty" errors
// into our MissingVarError, leaving other errors wrapped as-is.
func translateParseError(err error) error {
	if name, ok := extractMissingVar(err); ok {
		return fmt.Errorf("config: %w", &MissingVarError{Var: name})
	}
	return fmt.Errorf("config: %w", err)
}

// extractMissingVar inspects an env.Parse error and, when it represents a
// missing/empty required variable, returns the variable name.
func extractMissingVar(err error) (string, bool) {
	var notSet env.VarIsNotSetError
	if errors.As(err, &notSet) {
		return notSet.Key, true
	}
	var emptyVar env.EmptyVarError
	if errors.As(err, &emptyVar) {
		return emptyVar.Key, true
	}
	// env.Parse joins multiple errors. Walk the tree manually because errors.As
	// only matches the first leaf of a particular type.
	if u, ok := err.(interface{ Unwrap() []error }); ok {
		for _, e := range u.Unwrap() {
			if n, ok := extractMissingVar(e); ok {
				return n, true
			}
		}
	}
	if name := guessVarFromMessage(err.Error()); name != "" {
		return name, true
	}
	return "", false
}

// guessVarFromMessage is the last-resort fallback for env library variations
// that don't expose typed errors. It looks for "required environment variable
// X is not set" patterns.
func guessVarFromMessage(msg string) string {
	const marker = `required environment variable "`
	i := strings.Index(msg, marker)
	if i < 0 {
		return ""
	}
	rest := msg[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}
