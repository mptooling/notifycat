# config.yaml Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move all non-secret configuration into a single `config.yaml`, shrink `.env` to secrets + infra-interpolation values, retarget the validation cache to `config.lock`, and rename `notifycat-mapping` → `notifycat-config` — while preserving today's per-org mappings behavior.

**Architecture:** `internal/config` decodes `config.yaml` through an internal `fileSchema` (mirrors the nested YAML) and maps it onto the existing flat `config.Config` (field names unchanged). Secrets are read from the environment by fixed name and never appear in YAML. The `mappings:` and `digest:` sections decode into the existing `mappings.Org` / `mappings.DigestConfig` types (reused verbatim), and a new `mappings.NewProvider` builds the provider from those in-memory values instead of reading a separate file. The lock file machinery is unchanged; only its path derives from `config.yaml` (→ `config.lock`).

**Tech Stack:** Go 1.25.10, `gopkg.in/yaml.v3` (already a dependency), `github.com/joho/godotenv` (already present, now secrets-only), GORM/SQLite (unchanged), `just` for tasks.

## Global Constraints

- Go toolchain pinned at **1.25.10**.
- This is a **breaking change**; bump the minor version (`0.16` → `0.17`, pre-1.0 rule).
- **No Claude attribution** in commits or PRs (no `Co-Authored-By`, no footer).
- **No hard-wrapped markdown** in repo docs / PR bodies — let GFM wrap.
- Do **not** commit `config.yaml`, `config.lock`, `.env`, or anything under `/data/` — all gitignored operator state.
- **One constructor per type, all deps injected** — never split a type into a prod-wiring façade plus a test-seam constructor.
- **Consumer-package interfaces** — interfaces live where consumed, not where implemented.
- **TDD**: RED → verify failure → GREEN → REFACTOR. New behavior starts with a failing test.
- **No comments restating code** — only document non-obvious *why*.
- Verify with `just check` (vet + lint + vuln + race tests + build) before declaring done.
- Secrets always use the `config.Secret` type so they cannot leak through logs.

---

### Task 1: `mappings.NewProvider` constructor

Build a `*mappings.Provider` from already-decoded in-memory values, so `config` can hand it the `mappings:` / `digest:` sections of `config.yaml` instead of a file path. `mappings.Load` (file-reading) stays for now; later tasks migrate callers off it.

**Files:**
- Modify: `internal/mappings/provider.go`
- Test: `internal/mappings/provider_test.go`

**Interfaces:**
- Consumes: `mappings.File{ Digest *DigestConfig; Mappings map[string]Org }` (existing, `internal/mappings/types.go`), `mappings.Org` (existing).
- Produces: `func NewProvider(m map[string]Org, digest *DigestConfig) *Provider`. The returned provider's `Get`, `Entries`, and `Digest` behave identically to one built via `Load`.

- [ ] **Step 1: Write the failing test**

Add to `internal/mappings/provider_test.go`:

```go
func TestNewProvider_BehavesLikeLoad(t *testing.T) {
	m := map[string]mappings.Org{
		"acme": {
			Channel:         "C0123ABCDE",
			Mentions:        []string{"<@U1>"},
			MentionsPresent: true,
			Repositories:    mappings.Repositories{List: []string{"web"}},
		},
	}
	p := mappings.NewProvider(m, nil)

	got, err := p.Get(context.Background(), "acme/web")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SlackChannel != "C0123ABCDE" {
		t.Errorf("SlackChannel = %q; want C0123ABCDE", got.SlackChannel)
	}
	// nil digest → feature on by default with the default schedule.
	if d := p.Digest(); !d.Enabled || d.Schedule != mappings.DefaultDigestSchedule {
		t.Errorf("Digest() = %+v; want enabled with default schedule", d)
	}
	if len(p.Entries()) != 1 {
		t.Errorf("Entries() = %d; want 1", len(p.Entries()))
	}
}
```

If `internal/mappings/provider_test.go` lacks the `context` import, add it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mappings/ -run TestNewProvider_BehavesLikeLoad`
Expected: FAIL — `undefined: mappings.NewProvider`.

- [ ] **Step 3: Add the constructor**

In `internal/mappings/provider.go`, immediately after the `Load` function (around line 31), add:

```go
// NewProvider builds a Provider from already-decoded sections (config.yaml's
// `mappings:` map and `digest:` block), the in-memory counterpart to Load.
// A nil digest leaves the feature on by default (see Digest).
func NewProvider(m map[string]Org, digest *DigestConfig) *Provider {
	return &Provider{file: File{Mappings: m, Digest: digest}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mappings/ -run TestNewProvider_BehavesLikeLoad`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mappings/provider.go internal/mappings/provider_test.go
git commit -m "feat: add mappings.NewProvider for in-memory construction"
```

---

### Task 2: Rewrite `internal/config` to load `config.yaml`

Replace env-driven app config with `config.yaml`. Keep the flat `Config` field names so downstream consumers barely change. Read secrets from env. Detect retired env vars and fail fast.

**Files:**
- Modify: `internal/config/config.go` (full rewrite of the load path; keep `Secret` in `secret.go` untouched)
- Test: `internal/config/config_test.go` (full rewrite)
- Test helper: `internal/config/helpers_test.go` (leave as-is unless a helper is unused)

**Interfaces:**
- Consumes: `config.Secret` (existing), `mappings.Org`, `mappings.DigestConfig` (existing).
- Produces:
  - `type Config struct` with **unchanged** flat fields: `Addr, LogLevel, LogFormat, DatabaseURL string`; `Reactions Reactions`; `SlackBaseURL, GitHubBaseURL, Domain string`; `MessageTTLDays int`; `IgnoreAIReviews, DependabotFormat bool`; `GitHubWebhookSecret, SlackBotToken, GitHubToken Secret`. **Removed:** `MappingsFile`. **Added:** `ConfigFile string`, `Mappings map[string]mappings.Org`, `Digest *mappings.DigestConfig`.
  - `func Load() (Config, error)` — unchanged signature.
  - `type MissingVarError struct{ Var string }` with `Error() string` — unchanged (still used by doctor + tests).

- [ ] **Step 1: Write the failing tests**

Replace the entire contents of `internal/config/config_test.go` with:

```go
package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mptooling/notifycat/internal/config"
)

// writeConfig writes a config.yaml into a temp dir, points NOTIFYCAT_CONFIG_FILE
// at it, and clears every secret + retired env var so each test starts clean.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("NOTIFYCAT_CONFIG_FILE", path)
	for _, k := range []string{
		"ADDR", "LOG_LEVEL", "LOG_FORMAT", "DATABASE_URL", "NOTIFYCAT_MAPPINGS_FILE",
		"SLACK_BASE_URL", "GITHUB_BASE_URL", "NOTIFYCAT_MESSAGE_TTL_DAYS",
		"NOTIFYCAT_IGNORE_AI_REVIEWS", "NOTIFYCAT_DEPENDABOT_FORMAT",
		"SLACK_REACTIONS_ENABLED", "SLACK_REACTION_NEW_PR",
		"GITHUB_WEBHOOK_SECRET", "SLACK_BOT_TOKEN", "GITHUB_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

const minimalConfig = "server:\n  log_level: info\n"

func setSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", "shh")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")
}

func TestLoad_RequiresWebhookSecret(t *testing.T) {
	writeConfig(t, minimalConfig)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-x")

	_, err := config.Load()
	var missing *config.MissingVarError
	if !errors.As(err, &missing) || missing.Var != "GITHUB_WEBHOOK_SECRET" {
		t.Fatalf("Load() error = %v; want MissingVarError(GITHUB_WEBHOOK_SECRET)", err)
	}
}

func TestLoad_RequiresSlackBotToken(t *testing.T) {
	writeConfig(t, minimalConfig)
	t.Setenv("GITHUB_WEBHOOK_SECRET", "shh")

	_, err := config.Load()
	var missing *config.MissingVarError
	if !errors.As(err, &missing) || missing.Var != "SLACK_BOT_TOKEN" {
		t.Fatalf("Load() error = %v; want MissingVarError(SLACK_BOT_TOKEN)", err)
	}
}

func TestLoad_AppliesDefaultsForAbsentKeys(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Addr", cfg.Addr, ":8080"},
		{"LogLevel", cfg.LogLevel, "info"},
		{"LogFormat", cfg.LogFormat, "text"},
		{"DatabaseURL", cfg.DatabaseURL, "file:./data/notifycat.db"},
		{"SlackBaseURL", cfg.SlackBaseURL, "https://slack.com"},
		{"GitHubBaseURL", cfg.GitHubBaseURL, "https://api.github.com"},
		{"Reactions.Enabled", cfg.Reactions.Enabled, true},
		{"Reactions.NewPR", cfg.Reactions.NewPR, "eyes"},
		{"Reactions.MergedPR", cfg.Reactions.MergedPR, "twisted_rightwards_arrows"},
		{"Reactions.ClosedPR", cfg.Reactions.ClosedPR, "x"},
		{"Reactions.Approved", cfg.Reactions.Approved, "white_check_mark"},
		{"Reactions.Commented", cfg.Reactions.Commented, "speech_balloon"},
		{"Reactions.RequestChange", cfg.Reactions.RequestChange, "exclamation"},
		{"Reactions.BotReview", cfg.Reactions.BotReview, "robot_face"},
		{"MessageTTLDays", cfg.MessageTTLDays, 30},
		{"IgnoreAIReviews", cfg.IgnoreAIReviews, false},
		{"DependabotFormat", cfg.DependabotFormat, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v; want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_OverridesAndMappings(t *testing.T) {
	writeConfig(t, `
server:
  addr: ":9000"
  log_level: debug
  log_format: json
  domain: notifycat.example.com
database:
  url: "file:/tmp/custom.db"
slack:
  reactions:
    enabled: false
    new_pr: rocket
reviews:
  ignore_ai_reviews: true
  dependabot_format: false
cleanup:
  message_ttl_days: 7
digest:
  enabled: false
mappings:
  acme:
    channel: C0123ABCDE
    repositories:
      - web
`)
	setSecrets(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != ":9000" || cfg.LogLevel != "debug" || cfg.LogFormat != "json" {
		t.Errorf("server overrides not applied: %+v", cfg)
	}
	if cfg.Domain != "notifycat.example.com" {
		t.Errorf("Domain = %q", cfg.Domain)
	}
	if cfg.DatabaseURL != "file:/tmp/custom.db" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.Reactions.Enabled {
		t.Errorf("Reactions.Enabled = true; want false")
	}
	if cfg.Reactions.NewPR != "rocket" {
		t.Errorf("Reactions.NewPR = %q", cfg.Reactions.NewPR)
	}
	if cfg.Reactions.MergedPR != "twisted_rightwards_arrows" {
		t.Errorf("Reactions.MergedPR default lost = %q", cfg.Reactions.MergedPR)
	}
	if !cfg.IgnoreAIReviews || cfg.DependabotFormat {
		t.Errorf("reviews overrides not applied: ignore=%v dependabot=%v", cfg.IgnoreAIReviews, cfg.DependabotFormat)
	}
	if cfg.MessageTTLDays != 7 {
		t.Errorf("MessageTTLDays = %d; want 7", cfg.MessageTTLDays)
	}
	if cfg.Digest == nil || cfg.Digest.Enabled {
		t.Errorf("Digest = %+v; want non-nil, disabled", cfg.Digest)
	}
	if _, ok := cfg.Mappings["acme"]; !ok {
		t.Errorf("Mappings missing acme: %+v", cfg.Mappings)
	}
}

func TestLoad_MessageTTLDays_RejectsZero(t *testing.T) {
	writeConfig(t, "cleanup:\n  message_ttl_days: 0\n")
	setSecrets(t)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with message_ttl_days=0; want error")
	}
}

func TestLoad_MissingFileIsError(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("NOTIFYCAT_CONFIG_FILE", filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with missing config.yaml; want error")
	}
}

func TestLoad_UnknownKeyRejected(t *testing.T) {
	writeConfig(t, "server:\n  not_a_real_key: x\n")
	setSecrets(t)
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with an unknown key; want error")
	}
}

func TestLoad_RetiredEnvVarRejected(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	t.Setenv("LOG_LEVEL", "debug") // retired: app config now lives in config.yaml
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() succeeded with a retired env var set; want error pointing to migration")
	}
}

func TestLoad_SecretsAreSecretType(t *testing.T) {
	writeConfig(t, minimalConfig)
	setSecrets(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.GitHubWebhookSecret.Reveal() != "shh" || cfg.SlackBotToken.Reveal() != "xoxb-x" {
		t.Error("secrets not read from env")
	}
	if cfg.GitHubWebhookSecret.String() == "shh" {
		t.Error("secret renders raw via String()")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/`
Expected: FAIL to compile / run — `config.yaml` loading and the new fields don't exist yet.

- [ ] **Step 3: Rewrite `internal/config/config.go`**

Replace the entire file with:

```go
// Package config loads runtime configuration from a single config.yaml file.
// Secrets are read separately from the environment (optionally via a .env file
// in development) and never appear in the YAML.
//
// All secret values use the Secret type so they cannot leak through logging.
package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"

	"github.com/mptooling/notifycat/internal/mappings"
)

// Config is the parsed runtime configuration. Field names are flat so consumers
// read cfg.Addr, cfg.Reactions.NewPR, etc.; the nested config.yaml shape is an
// internal detail of Load (see fileSchema).
type Config struct {
	// ConfigFile is the path config.yaml was loaded from; the sibling
	// config.lock is derived from it.
	ConfigFile string

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

	Digest   *mappings.DigestConfig
	Mappings map[string]mappings.Org

	GitHubWebhookSecret Secret
	SlackBotToken       Secret
	GitHubToken         Secret
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
	Server struct {
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
			Enabled       *bool  `yaml:"enabled"`
			NewPR         string `yaml:"new_pr"`
			MergedPR      string `yaml:"merged_pr"`
			ClosedPR      string `yaml:"closed_pr"`
			Approved      string `yaml:"approved"`
			Commented     string `yaml:"commented"`
			RequestChange string `yaml:"request_change"`
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
	Digest   *mappings.DigestConfig  `yaml:"digest"`
	Mappings map[string]mappings.Org `yaml:"mappings"`
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
	setString(&cfg.Addr, fs.Server.Addr)
	setString(&cfg.LogLevel, fs.Server.LogLevel)
	setString(&cfg.LogFormat, fs.Server.LogFormat)
	cfg.Domain = fs.Server.Domain // optional; empty is a valid value
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

func setString(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}

// readSecrets pulls the three secrets from the environment into cfg. The two
// required ones produce a MissingVarError when unset/empty.
func readSecrets(cfg *Config) error {
	cfg.GitHubWebhookSecret = Secret(os.Getenv("GITHUB_WEBHOOK_SECRET"))
	cfg.SlackBotToken = Secret(os.Getenv("SLACK_BOT_TOKEN"))
	cfg.GitHubToken = Secret(os.Getenv("GITHUB_TOKEN"))
	if cfg.GitHubWebhookSecret.Reveal() == "" {
		return fmt.Errorf("config: %w", &MissingVarError{Var: "GITHUB_WEBHOOK_SECRET"})
	}
	if cfg.SlackBotToken.Reveal() == "" {
		return fmt.Errorf("config: %w", &MissingVarError{Var: "SLACK_BOT_TOKEN"})
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/`
Expected: PASS (all tests).

- [ ] **Step 5: Tidy and verify the package builds**

Run: `go vet ./internal/config/ && go build ./internal/config/`
Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: load non-secret config from config.yaml, secrets from env"
```

---

### Task 3: Rewire `app.Wire` to build the provider from config

`app.Wire` and `startupValidate` must build the provider via `mappings.NewProvider(cfg.Mappings, cfg.Digest)` and derive the lock path from `cfg.ConfigFile`.

**Files:**
- Modify: `internal/app/app.go:43` (provider construction) and `internal/app/app.go:146` (lock path)
- Test: `internal/app/integration_test.go:241,250` (fixture now writes config.yaml + uses cfg.ConfigFile)

**Interfaces:**
- Consumes: `config.Config{ Mappings, Digest, ConfigFile }` (Task 2), `mappings.NewProvider` (Task 1).
- Produces: no signature change to `Wire`.

- [ ] **Step 1: Update provider construction**

In `internal/app/app.go`, replace lines 43-46:

```go
	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("app: load mappings: %w", err)
	}
```

with:

```go
	provider := mappings.NewProvider(cfg.Mappings, cfg.Digest)
```

Then update the doc comment at `internal/app/app.go:38-39` (the "Mappings come from the declarative cfg.MappingsFile" sentence) to:

```go
// Mappings come from the `mappings:` section of config.yaml; the server
// refuses to start if any entry fails validation (against the per-entry lock).
```

- [ ] **Step 2: Update the lock path**

In `internal/app/app.go` `startupValidate`, replace line 146:

```go
	lockPath := mappings.LockPath(cfg.MappingsFile)
```

with:

```go
	lockPath := mappings.LockPath(cfg.ConfigFile)
```

- [ ] **Step 3: Update the integration test fixture**

Open `internal/app/integration_test.go`. Find where it builds a `config.Config` and writes a mappings file (around lines 241-250, using `mappings.Load(mappingsPath)` and `mappings.LockPath(mappingsPath)`). Change the fixture so it:
  - sets `cfg.ConfigFile` to a temp `config.yaml` path (the lock will sit beside it as `config.lock`),
  - populates `cfg.Mappings` directly (a `map[string]mappings.Org`) instead of writing a separate mappings file,
  - removes the now-invalid `cfg.MappingsFile` assignment.

Concretely, replace the mappings-file setup:

```go
	p, err := mappings.Load(mappingsPath)
	// ...
	if err := mappings.WriteLock(mappings.LockPath(mappingsPath), lock); err != nil {
```

with construction from an in-memory map and a `config.yaml`-derived lock path:

```go
	configPath := filepath.Join(dir, "config.yaml")
	cfg.ConfigFile = configPath
	cfg.Mappings = map[string]mappings.Org{
		"acme": {
			Channel:      "C0123ABCDE",
			Repositories: mappings.Repositories{List: []string{"web"}},
		},
	}
	p := mappings.NewProvider(cfg.Mappings, cfg.Digest)
	// ... build `lock` from p.Entries() as before ...
	if err := mappings.WriteLock(mappings.LockPath(configPath), lock); err != nil {
```

Keep the rest of the test body identical (it asserts on `p.Entries()` / webhook delivery). If the test references `cfg.MappingsFile` anywhere else, delete those lines. Ensure `path/filepath` is imported.

- [ ] **Step 4: Run the app tests**

Run: `go test ./internal/app/`
Expected: PASS. If a compile error mentions `cfg.MappingsFile`, remove that reference.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/app/integration_test.go
git commit -m "feat: build mappings provider from config.yaml in app.Wire"
```

---

### Task 4: Update the doctor to read mappings from config

`CheckMappingsFile(path)` loaded a separate file; it now inspects the provider built from config. `CheckConfig` should report `config.yaml` instead of `NOTIFYCAT_MAPPINGS_FILE`. The doctor's `buildValidator` builds the provider from config too.

**Files:**
- Modify: `internal/doctor/mappings_check.go` (rename + reshape `CheckMappingsFile`)
- Modify: `internal/doctor/config_check.go:31-35` (replace the `MappingsFile` block)
- Modify: `cmd/notifycat-doctor/main.go:55-67` (`buildValidator`)
- Modify: wherever `CheckMappingsFile` is called (search `doctor` package + its `Run`)
- Test: `internal/doctor/mappings_check_test.go` and `internal/doctor/config_check_test.go` if present (adapt to the new signature)

**Interfaces:**
- Consumes: `config.Config{ Mappings, Digest, ConfigFile }`, `mappings.NewProvider`.
- Produces: `func CheckMappings(provider *mappings.Provider) Section` (replaces `CheckMappingsFile(path string)`).

- [ ] **Step 1: Locate the caller**

Run: `grep -rn "CheckMappingsFile\|MappingsFile" internal/doctor cmd/notifycat-doctor`
Expected: the definition in `mappings_check.go`, a call in the doctor's `Run`/aggregator, and the `config_check.go` block. Note each line.

- [ ] **Step 2: Reshape `CheckMappings`**

Replace `internal/doctor/mappings_check.go` with:

```go
package doctor

import (
	"fmt"

	"github.com/mptooling/notifycat/internal/mappings"
)

// CheckMappings reports whether the `mappings:` section of config.yaml parsed
// into any entries. An empty section is OK (the server boots but routes
// nothing). Parse failures already surface in config load, so by the time the
// doctor has a provider the file is structurally valid.
func CheckMappings(provider *mappings.Provider) Section {
	sec := Section{Name: "mappings"}
	entries := provider.Entries()
	if len(entries) == 0 {
		sec.Checks = append(sec.Checks, okResult("entries", "0 entries (server will boot but route nothing)"))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("entries", fmt.Sprintf("%d entries", len(entries))))
	return sec
}
```

- [ ] **Step 3: Update the doctor aggregator and `CheckConfig`**

In `internal/doctor/config_check.go`, replace lines 31-35 (the `MappingsFile` block) with a `config.yaml` report:

```go
	if cfg.ConfigFile == "" {
		sec.Checks = append(sec.Checks, failResult("config.yaml", "missing"))
	} else {
		sec.Checks = append(sec.Checks, okResult("config.yaml", cfg.ConfigFile))
	}
```

In the doctor's `Run` (search result from Step 1, the file that calls `CheckMappingsFile(cfg.MappingsFile)`), build the provider from config and pass it:

```go
	provider := mappings.NewProvider(cfg.Mappings, cfg.Digest)
	sections = append(sections, CheckMappings(provider))
```

Add the `mappings` import to that file if absent.

- [ ] **Step 4: Update `buildValidator`**

In `cmd/notifycat-doctor/main.go`, replace the body of `buildValidator` (lines 55-67) so it builds the provider from config:

```go
func buildValidator(cfg config.Config) doctor.RepoValidator {
	provider := mappings.NewProvider(cfg.Mappings, cfg.Digest)
	hc := &http.Client{Timeout: 10 * time.Second}
	slackClient := slack.NewClient(hc, cfg.SlackBotToken.Reveal(), slack.WithBaseURL(cfg.SlackBaseURL))
	var ghChecker validate.GitHubChecker
	if cfg.GitHubToken.Reveal() != "" {
		ghChecker = github.NewClient(hc, cfg.GitHubToken.Reveal(), github.WithBaseURL(cfg.GitHubBaseURL))
	}
	return validate.NewValidator(provider, slackClient, ghChecker)
}
```

(The doc comment above it that mentions "when the mappings file cannot be loaded" no longer applies — replace it with a one-line note that the provider always builds from in-memory config.)

- [ ] **Step 5: Adapt doctor tests**

Run: `go test ./internal/doctor/ ./cmd/notifycat-doctor/`
For each failure: a test calling `CheckMappingsFile(path)` becomes `CheckMappings(mappings.NewProvider(m, nil))` with an in-memory map; a test asserting on `NOTIFYCAT_MAPPINGS_FILE` becomes `config.yaml`; a test setting `cfg.MappingsFile` sets `cfg.ConfigFile` + `cfg.Mappings`. Apply these mechanically until green.

- [ ] **Step 6: Verify**

Run: `go test ./internal/doctor/ ./cmd/notifycat-doctor/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/doctor cmd/notifycat-doctor
git commit -m "feat: doctor reads mappings from config.yaml"
```

---

### Task 5: Update the remaining binaries off `mappings.Load`/`MappingsFile`

`notifycat-smoke` and any other binary still calls `mappings.Load(cfg.MappingsFile)`. Repoint them at the provider built from config. `notifycat-migrate` and `notifycat-reconcile` only use `config.Load()` + `cfg.DatabaseURL` and need no change beyond confirming they compile.

**Files:**
- Modify: `cmd/notifycat-smoke/main.go:43-50`
- Verify: `cmd/notifycat-migrate/main.go`, `cmd/notifycat-reconcile/main.go` (compile only)

**Interfaces:**
- Consumes: `config.Config{ Mappings, Digest }`, `mappings.NewProvider`.

- [ ] **Step 1: Find lingering references**

Run: `grep -rn "MappingsFile\|mappings.Load" cmd/`
Expected: only `cmd/notifycat-smoke/main.go` (the mapping CLI is handled in Task 6).

- [ ] **Step 2: Update smoke**

In `cmd/notifycat-smoke/main.go`, replace:

```go
	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		// ...existing error handling...
	}
```

with:

```go
	provider := mappings.NewProvider(cfg.Mappings, cfg.Digest)
```

Remove the now-dead error branch and any now-unused imports (`errors` if it was only for this).

- [ ] **Step 3: Build all binaries**

Run: `go build ./cmd/...`
Expected: exit 0. Fix any remaining `cfg.MappingsFile` references the same way.

- [ ] **Step 4: Commit**

```bash
git add cmd/notifycat-smoke
git commit -m "feat: smoke binary builds provider from config.yaml"
```

---

### Task 6: Rename `notifycat-mapping` → `notifycat-config`

Rename the binary directory and update its usage strings and doc comment. The `internal/mappingcli` package and validation pipeline are unchanged.

**Files:**
- Rename: `cmd/notifycat-mapping/` → `cmd/notifycat-config/` (both `main.go` and `main_test.go`)
- Modify: usage strings + the `cfg.MappingsFile`/`mappings.Load` calls inside it (lines 31, 41) → config-built provider + `cfg.ConfigFile`

**Interfaces:**
- Consumes: `config.Config{ Mappings, Digest, ConfigFile }`, `mappings.NewProvider`, `mappings.LockPath`.

- [ ] **Step 1: Move the directory with git**

```bash
git mv cmd/notifycat-mapping cmd/notifycat-config
```

- [ ] **Step 2: Update provider construction + lock path**

In `cmd/notifycat-config/main.go`, replace:

```go
	provider, err := mappings.Load(cfg.MappingsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
```

with:

```go
	provider := mappings.NewProvider(cfg.Mappings, cfg.Digest)
```

and replace `mappings.LockPath(cfg.MappingsFile)` (line ~41) with `mappings.LockPath(cfg.ConfigFile)`.

- [ ] **Step 3: Update program name strings**

In `cmd/notifycat-config/main.go`: change the package doc comment's `notifycat-mapping` references to `notifycat-config`, the `fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)` prefixes to `"notifycat-config:"`, and the `usage()` string body:

```go
func usage() string {
	return strings.TrimSpace(`
usage:
  notifycat-config list
  notifycat-config validate [owner/repo] [--force]
`)
}
```

- [ ] **Step 4: Update the test**

In `cmd/notifycat-config/main_test.go`, replace its `mappings.Load(path)` fixture (line ~30) with `mappings.NewProvider(m, nil)` built from an in-memory `map[string]mappings.Org`, and update any `notifycat-mapping` string assertions to `notifycat-config`.

- [ ] **Step 5: Verify**

Run: `go test ./cmd/notifycat-config/ && go build ./cmd/notifycat-config/`
Expected: PASS, exit 0.

- [ ] **Step 6: Commit**

```bash
git add cmd/notifycat-config
git commit -m "feat: rename notifycat-mapping to notifycat-config"
```

---

### Task 7: Update build + ops wiring (Dockerfile, justfile, mkdocs)

Point the image build, dev tasks, and docs nav at `notifycat-config`, and stop referencing the separate mappings file.

**Files:**
- Modify: `Dockerfile` (lines 20, 37 — the `notifycat-mapping` build/copy)
- Modify: `justfile` (the `mapping-*` recipes around lines 64-100)
- Modify: `mkdocs.yml` (any `notifycat-mapping` nav/reference)

- [ ] **Step 1: Dockerfile**

In `Dockerfile`, change the two `notifycat-mapping` lines:

```dockerfile
RUN go build -ldflags="${LDFLAGS}" -o /out/notifycat-config  ./cmd/notifycat-config
```
```dockerfile
COPY --from=build /out/notifycat-config   /usr/local/bin/notifycat-config
```

- [ ] **Step 2: justfile**

In `justfile`, rename every `notifycat-mapping` recipe and invocation to `notifycat-config`. Delete the `mapping-add` and `mapping-remove` recipes (lines ~64, ~72) — those `add`/`remove` subcommands never existed in the binary (they hit the default "unknown subcommand" branch). Rename `mapping-list` → `config-list`, `mapping-validate` → `config-validate`, and the docker variants likewise, each calling `/usr/local/bin/notifycat-config`.

- [ ] **Step 3: mkdocs**

Run: `grep -n "notifycat-mapping\|mappings.yaml\|MAPPINGS_FILE" mkdocs.yml`
Replace `notifycat-mapping` references with `notifycat-config`. (Doc *content* changes land in Task 8; here only the nav/keys in `mkdocs.yml`.)

- [ ] **Step 4: Verify the image builds**

Run: `just docker-build`
Expected: image builds, both `notifycat-config` and the server binaries present. If `just docker-build` is unavailable in the environment, instead run `go build -o /dev/null ./cmd/notifycat-config` and visually confirm the Dockerfile/justfile edits.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile justfile mkdocs.yml
git commit -m "build: wire notifycat-config into image, tasks, and docs nav"
```

---

### Task 8: Docs, examples, and the migration guide

Rewrite operator-facing config docs for the single-file model, add `config.example.yaml`, trim `.env.example`, retire `mappings.example.yaml`, and write the manual `0.17` migration guide.

**Files:**
- Create: `config.example.yaml`
- Create: `docs/0.17-config-migration.md`
- Modify: `.env.example` (trim to secrets + infra)
- Modify: `docs/configuration.md` (rewrite for config.yaml)
- Modify: `docs/mappings.md` (note the section now lives in config.yaml)
- Delete: `mappings.example.yaml` (fold commentary into `config.example.yaml`)
- Modify: `.gitignore` (add `config.yaml`, `config.lock`; the old `mappings.yaml`/`mappings.lock` ignores can stay or go)

- [ ] **Step 1: Add `config.example.yaml`**

Create `config.example.yaml` with every section, defaults shown and commented, plus the per-org mappings examples carried over from `mappings.example.yaml` (acme explicit list, beta `repositories: "*"`, gamma `<!channel>`, legacy `mentions: []`). Lead with a header noting secrets live in `.env`. Use the schema from `docs/superpowers/specs/2026-06-24-config-yaml-and-per-repo-mappings-design.md` (the "config.yaml schema" section), minus per-repo nesting (this plan keeps per-org `repositories:`).

- [ ] **Step 2: Trim `.env.example`**

Reduce `.env.example` to only: `GITHUB_WEBHOOK_SECRET`, `SLACK_BOT_TOKEN`, `GITHUB_TOKEN` (optional), `DOMAIN`, `ACME_EMAIL`. Add a top comment: "Secrets and infra-interpolation only. All other configuration lives in config.yaml — see config.example.yaml." Remove every `ADDR`/`LOG_*`/`DATABASE_URL`/`NOTIFYCAT_*`/`SLACK_REACTION*`/`SLACK_BASE_URL`/`GITHUB_BASE_URL`/`NOTIFYCAT_MAPPINGS_FILE` line.

- [ ] **Step 3: Write the migration guide**

Create `docs/0.17-config-migration.md` containing:
  - A short intro: 0.17 consolidates non-secret config into `config.yaml`; this is a manual, breaking migration.
  - A table mapping every retired env var → its `config.yaml` path (e.g. `LOG_LEVEL` → `server.log_level`, `SLACK_REACTION_NEW_PR` → `slack.reactions.new_pr`, `NOTIFYCAT_MESSAGE_TTL_DAYS` → `cleanup.message_ttl_days`, `NOTIFYCAT_MAPPINGS_FILE` → removed / mappings now in `config.yaml`).
  - The "stays in `.env`" list: `GITHUB_WEBHOOK_SECRET`, `SLACK_BOT_TOKEN`, `GITHUB_TOKEN`, `DOMAIN`, `ACME_EMAIL`.
  - Mappings: move `mappings.yaml`'s `mappings:` / `digest:` content under `config.yaml`'s top-level `mappings:` / `digest:` keys (same per-org schema).
  - Note: the server now fails fast if a retired env var is still set, pointing back here.
  - CLI: `notifycat-mapping` → `notifycat-config`.

- [ ] **Step 4: Rewrite `docs/configuration.md`**

Replace the "reads configuration from environment variables" framing with: non-secret config is `config.yaml` (path via `NOTIFYCAT_CONFIG_FILE`, default `./config.yaml`); secrets + `DOMAIN`/`ACME_EMAIL` are env/`.env`. Convert the per-variable tables into the YAML key tables. Keep the secrets table (`GITHUB_WEBHOOK_SECRET`, `SLACK_BOT_TOKEN`, `GITHUB_TOKEN`). Update the CLI section to `notifycat-config`.

- [ ] **Step 5: Update `docs/mappings.md` + `.gitignore`**

In `docs/mappings.md`, add a note at the top that mappings now live in the `mappings:` section of `config.yaml` (schema otherwise unchanged in 0.17), and that `notifycat-config list|validate` operates on it. In `.gitignore`, add `config.yaml` and `config.lock`.

- [ ] **Step 6: Delete the old example**

```bash
git rm mappings.example.yaml
```

- [ ] **Step 7: Verify docs build (if mkdocs is available)**

Run: `grep -rn "notifycat-mapping\|mappings.yaml" docs/ README.md`
Expected: only intentional references (migration guide's "before" examples). Fix stragglers.

- [ ] **Step 8: Commit**

```bash
git add config.example.yaml docs .env.example .gitignore
git commit -m "docs: config.yaml model, examples, and 0.17 migration guide"
```

---

### Task 9: Full verification

- [ ] **Step 1: Run the whole suite with the race detector**

Run: `just check`
Expected: vet clean, lint clean, vuln clean, all tests PASS, all binaries build.

- [ ] **Step 2: Smoke-test a real boot locally**

Create a throwaway `config.yaml` (copy `config.example.yaml`, set a real channel id or leave `mappings:` empty) and a `.env` with dummy secrets, then:

Run: `NOTIFYCAT_CONFIG_FILE=./config.yaml just serve` (Ctrl-C after it logs `listening`).
Expected: server boots, validates (or no-ops on empty mappings), writes `config.lock` beside `config.yaml`. Confirm no `mappings.lock` is written.

- [ ] **Step 3: Confirm retired-var guard**

Run: `LOG_LEVEL=debug NOTIFYCAT_CONFIG_FILE=./config.yaml go run ./cmd/notifycat-server`
Expected: exits non-zero with the "no longer read … see docs/0.17-config-migration.md" message.

- [ ] **Step 4: Final commit (only if Steps 1-3 surfaced fixes)**

```bash
git add -A
git commit -m "test: verify config.yaml boot and retired-var guard"
```

---

## Self-Review

**Spec coverage (this plan = the config.yaml consolidation half of the spec):**
- Single `config.yaml`, only source for non-secret config → Tasks 2, 3.
- Secrets-only `.env` + infra interpolation (`DOMAIN`/`ACME_EMAIL`) → Tasks 2, 8.
- No env override (retired-var guard) → Task 2.
- `config.lock` retargeted → Tasks 3, 6.
- CLI rename → Tasks 6, 7.
- Manual migration guide, breaking 0.17 → Task 8 (+ Global Constraints).
- Per-repo mappings + full override + digest N-cron → **deferred to Plan 2** (explicitly out of scope here; per-org behavior preserved).

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to Task N" — each step has concrete code or an explicit grep-then-edit instruction. Tasks 3/4/5/6 contain `grep` discovery steps because exact line numbers in test/aggregator files depend on current contents; the edits themselves are spelled out.

**Type consistency:** `mappings.NewProvider(map[string]Org, *DigestConfig)` is defined in Task 1 and consumed identically in Tasks 3-6. `config.Config` flat fields are defined in Task 2 and read unchanged elsewhere. `CheckMappings(*mappings.Provider)` is defined and called consistently in Task 4. `cfg.ConfigFile` replaces `cfg.MappingsFile` everywhere (Tasks 2-6).
