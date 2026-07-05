// Package config loads runtime configuration from a single config.yaml file.
// Secrets are read separately from the environment (optionally via a .env file
// in development) and never appear in the YAML.
//
// All secret values use the Secret type so they cannot leak through logging.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"github.com/mptooling/notifycat/internal/kernel"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// Config is the parsed runtime configuration. Field names are flat so consumers
// read cfg.Addr, cfg.Reactions.NewPR, etc.; the nested config.yaml shape is an
// internal detail of Load (see fileSchema).
type Config struct {
	// ConfigFile is the path config.yaml was loaded from; the sibling
	// config.lock is derived from it.
	ConfigFile string

	// GitProvider is the required git_provider enum ("github"). It selects which
	// provider's secrets are required (see readSecrets) and hashes into every
	// lock entry (see routing Entry.Provider).
	GitProvider kernel.Provider

	Addr        string
	LogLevel    string
	LogFormat   string
	DatabaseURL string

	SlackBaseURL  string
	GitHubBaseURL string
	Domain        string

	MessageTTLDays   int
	IgnoreAIReviews  bool
	DependabotFormat bool

	Reactions Reactions

	Digest   *routingdomain.DigestConfig
	Mappings map[string]routingdomain.Org

	// DigestTimezone is the resolved timezone the digest scheduler and reporter
	// run in. Derived from Digest.Timezone at Load (default UTC); never nil.
	DigestTimezone *time.Location

	GitHubWebhookSecret Secret
	SlackBotToken       Secret
	GitHubToken         Secret

	// SlackSigningSecret gates the inbound Slack interactivity endpoint
	// (POST /webhook/slack/interactions). Optional: when empty the endpoint is
	// not registered and notifycat behaves exactly as an outbound-only service.
	SlackSigningSecret Secret
}

// Reactions configures Slack reaction emoji names per PR lifecycle event.
type Reactions struct {
	Enabled       bool
	NewPR         string
	MergedPR      string
	ClosedPR      string
	Approved      string
	Commented     string
	RequestChange string
	BotReview     string
}

// MissingVarError is returned when a required secret env var is unset or empty.
type MissingVarError struct{ Var string }

func (e *MissingVarError) Error() string {
	return fmt.Sprintf("config: required environment variable %s is missing", e.Var)
}

// DefaultConfigFile is used when NOTIFYCAT_CONFIG_FILE is unset.
const DefaultConfigFile = "./config.yaml"

// fileSchema mirrors config.yaml's nested shape; it exists only to decode the
// document. Bool/int leaves are pointers so an absent key is distinguishable
// from an explicit zero and the Go-side default survives.
type fileSchema struct {
	GitProvider kernel.Provider `yaml:"git_provider"`
	Server      struct {
		Addr      string `yaml:"addr"`
		LogLevel  string `yaml:"log_level"`
		LogFormat string `yaml:"log_format"`
		Domain    string `yaml:"domain"`
	} `yaml:"server"`
	Database struct {
		URL string `yaml:"url"`
	} `yaml:"database"`
	Slack struct {
		BaseURL   string `yaml:"base_url"`
		Reactions struct {
			Enabled       *bool   `yaml:"enabled"`
			NewPR         string  `yaml:"new_pr"`
			MergedPR      string  `yaml:"merged_pr"`
			ClosedPR      string  `yaml:"closed_pr"`
			Approved      string  `yaml:"approved"`
			Commented     string  `yaml:"commented"`
			RequestChange string  `yaml:"request_change"`
			BotReview     *string `yaml:"bot_review"`
		} `yaml:"reactions"`
	} `yaml:"slack"`
	GitHub struct {
		BaseURL string `yaml:"base_url"`
	} `yaml:"github"`
	Cleanup struct {
		MessageTTLDays *int `yaml:"message_ttl_days"`
	} `yaml:"cleanup"`
	Reviews struct {
		IgnoreAIReviews  *bool `yaml:"ignore_ai_reviews"`
		DependabotFormat *bool `yaml:"dependabot_format"`
	} `yaml:"reviews"`
	Digest   *routingdomain.DigestConfig  `yaml:"digest"`
	Mappings map[string]routingdomain.Org `yaml:"mappings"`
}

// defaults returns a Config pre-filled with every default value. Decode then
// overlays the file's present keys onto it.
func defaults() Config {
	return Config{
		Addr:             ":8080",
		LogLevel:         "info",
		LogFormat:        "text",
		DatabaseURL:      "file:./data/notifycat.db",
		SlackBaseURL:     "https://slack.com",
		GitHubBaseURL:    "https://api.github.com",
		MessageTTLDays:   30,
		IgnoreAIReviews:  false,
		DependabotFormat: true,
		Reactions: Reactions{
			Enabled:       true,
			NewPR:         "eyes",
			MergedPR:      "twisted_rightwards_arrows",
			ClosedPR:      "x",
			Approved:      "white_check_mark",
			Commented:     "speech_balloon",
			RequestChange: "exclamation",
			BotReview:     "robot_face",
		},
	}
}

// retiredVars are app-config env vars that moved into config.yaml. Setting one
// is almost always a stale .env from before the 0.17 migration; fail fast so it
// is not silently ignored. DOMAIN/ACME_EMAIL are intentionally absent — they
// stay in .env for docker-compose/Caddy.
var retiredVars = []string{
	"ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL", "NOTIFYCAT_MAPPINGS_FILE",
	"SLACK_BASE_URL", "GITHUB_BASE_URL", "NOTIFYCAT_MESSAGE_TTL_DAYS",
	"NOTIFYCAT_IGNORE_AI_REVIEWS", "NOTIFYCAT_DEPENDABOT_FORMAT",
	"SLACK_REACTIONS_ENABLED", "SLACK_REACTION_NEW_PR", "SLACK_REACTION_MERGED_PR",
	"SLACK_REACTION_CLOSED_PR", "SLACK_REACTION_PR_APPROVED",
	"SLACK_REACTION_PR_COMMENTED", "SLACK_REACTION_PR_REQUEST_CHANGE",
	"SLACK_REACTION_BOT_REVIEW",
}

// Load reads .env (secrets only; absent is fine), decodes config.yaml over the
// defaults, reads secrets from the environment, and validates.
func Load() (Config, error) {
	_ = godotenv.Load()

	if err := checkRetiredVars(); err != nil {
		return Config{}, err
	}

	path := os.Getenv("NOTIFYCAT_CONFIG_FILE")
	if path == "" {
		path = DefaultConfigFile
	}

	f, err := os.Open(path) //nolint:gosec // path is operator-supplied configuration
	if err != nil {
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var fs fileSchema
	if err := dec.Decode(&fs); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg := defaults()
	cfg.ConfigFile = path
	applyFileSchema(&cfg, fs)

	if err := validateGitProvider(cfg.GitProvider); err != nil {
		return Config{}, err
	}

	tz, err := resolveDigestTimezone(cfg.Digest)
	if err != nil {
		return Config{}, err
	}
	cfg.DigestTimezone = tz

	if err := routingapp.ValidateMappings(cfg.Mappings); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := readSecrets(&cfg); err != nil {
		return Config{}, err
	}
	if cfg.MessageTTLDays <= 0 {
		return Config{}, fmt.Errorf("config: cleanup.message_ttl_days must be > 0, got %d", cfg.MessageTTLDays)
	}
	return cfg, nil
}

// applyFileSchema overlays the file's present keys onto cfg (which starts at
// defaults). Empty strings and nil pointers mean "absent": keep the default.
func applyFileSchema(cfg *Config, fs fileSchema) {
	cfg.GitProvider = fs.GitProvider // required; validated after overlay
	setString(&cfg.Addr, fs.Server.Addr)
	setString(&cfg.LogLevel, fs.Server.LogLevel)
	setString(&cfg.LogFormat, fs.Server.LogFormat)
	cfg.Domain = fs.Server.Domain // no default; take the YAML value verbatim (empty is valid)
	setString(&cfg.DatabaseURL, fs.Database.URL)
	setString(&cfg.SlackBaseURL, fs.Slack.BaseURL)
	setString(&cfg.GitHubBaseURL, fs.GitHub.BaseURL)

	r := fs.Slack.Reactions
	if r.Enabled != nil {
		cfg.Reactions.Enabled = *r.Enabled
	}
	setString(&cfg.Reactions.NewPR, r.NewPR)
	setString(&cfg.Reactions.MergedPR, r.MergedPR)
	setString(&cfg.Reactions.ClosedPR, r.ClosedPR)
	setString(&cfg.Reactions.Approved, r.Approved)
	setString(&cfg.Reactions.Commented, r.Commented)
	setString(&cfg.Reactions.RequestChange, r.RequestChange)
	if r.BotReview != nil { // empty string is a meaningful value (no bot marker)
		cfg.Reactions.BotReview = *r.BotReview
	}

	if fs.Cleanup.MessageTTLDays != nil {
		cfg.MessageTTLDays = *fs.Cleanup.MessageTTLDays
	}
	if fs.Reviews.IgnoreAIReviews != nil {
		cfg.IgnoreAIReviews = *fs.Reviews.IgnoreAIReviews
	}
	if fs.Reviews.DependabotFormat != nil {
		cfg.DependabotFormat = *fs.Reviews.DependabotFormat
	}
	cfg.Digest = fs.Digest
	cfg.Mappings = fs.Mappings
}

// resolveDigestTimezone turns the optional digest.timezone into a *time.Location.
// An absent/empty zone defaults to UTC; an unrecognized zone is a startup error,
// matching the fail-fast contract for an invalid cron spec. Named zones rely on
// the embedded tzdata (see tzdata.go) to resolve on the scratch image.
func resolveDigestTimezone(d *routingdomain.DigestConfig) (*time.Location, error) {
	name := "UTC"
	if d != nil {
		if z := strings.TrimSpace(d.Timezone); z != "" {
			name = z
		}
	}
	tz, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("config: digest.timezone %q: %w", name, err)
	}
	return tz, nil
}

func setString(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

// readSecrets pulls the secrets from the environment into cfg. SLACK_BOT_TOKEN is
// always required; the webhook secret requirement follows the selected provider
// (see requireProviderSecret). GITHUB_TOKEN and SLACK_SIGNING_SECRET are optional
// features.
func readSecrets(cfg *Config) error {
	cfg.GitHubWebhookSecret = Secret(os.Getenv("GITHUB_WEBHOOK_SECRET"))
	cfg.SlackBotToken = Secret(os.Getenv("SLACK_BOT_TOKEN"))
	cfg.GitHubToken = Secret(os.Getenv("GITHUB_TOKEN"))
	cfg.SlackSigningSecret = Secret(os.Getenv("SLACK_SIGNING_SECRET"))
	if cfg.SlackBotToken.Reveal() == "" {
		return fmt.Errorf("config: %w", &MissingVarError{Var: "SLACK_BOT_TOKEN"})
	}
	return requireProviderSecret(cfg)
}

// requireProviderSecret enforces the webhook secret of the selected git provider
// (D8): git_provider: github requires GITHUB_WEBHOOK_SECRET. A second provider's
// secret joins here as its inbound stack lands; a deployment never needs another
// provider's credential.
func requireProviderSecret(cfg *Config) error {
	switch cfg.GitProvider {
	case gitProviderGitHub:
		if cfg.GitHubWebhookSecret.Reveal() == "" {
			return fmt.Errorf("config: %w", &MissingVarError{Var: "GITHUB_WEBHOOK_SECRET"})
		}
	}
	return nil
}

func checkRetiredVars() error {
	var found []string
	for _, k := range retiredVars {
		if os.Getenv(k) != "" {
			found = append(found, k)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("config: these env vars are no longer read and now live in config.yaml: %v — see docs/0.17-config-migration.md", found)
}
