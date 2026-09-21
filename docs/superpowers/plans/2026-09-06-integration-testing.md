# Notifycat Integration Testing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an automated integration suite in `mptooling/notifycat-integration-tests` that drives real GitHub repositories through a real webhook delivery into the published notifycat container image and asserts the result in a real Slack workspace.

**Architecture:** A Go + testify harness. Three network-facing units (`githubx`, `slackx`, `harness`) are the only code that touches an API; scenarios in `tests/` read as prose against them. The notifycat container runs on the CI runner; a cloudflared quick tunnel gives GitHub a public URL to deliver to. Validation scenarios need no tunnel — they run the same image with a different command and assert on exit codes, stdout and `config.lock`.

**Tech Stack:** Go 1.25.14, testify, stdlib `net/http`, Docker CLI, `cloudflared`, GitHub Actions, `gh` CLI.

**Spec:** `docs/superpowers/specs/2026-09-06-integration-testing-design.md` (in the `mptooling/notifycat` repository — read it alongside this plan)

## Global Constraints

- Go toolchain **1.25.14**, matching notifycat.
- Every test uses **testify**. `require` for anything whose failure makes the rest of the test meaningless; `assert` for independent end-of-test checks. Argument order is `(t, want, got)`. Never `reflect.DeepEqual`.
- Table tests are `testCases := []struct{ name string; … }` driven through `t.Run(testCase.name, …)`. Loop variables spelled out (`testCase`, `testCases`), never `tc`.
- **Readable names over terse Go idiom.** `repoFullName`, not `r`. Loop indices and receivers may stay short.
- **No comments restating the code.** Comment only a non-obvious *why*.
- More than three arguments to an exported function or constructor → a single input struct.
- Commits use Conventional Commits (`type(scope): subject`, lowercase subject). **Never** mention Claude, Claude Code, or AI authorship in a commit message — no `Co-Authored-By`, no "Generated with" trailer.
- PR title = commit message; the notifycat repo lints it against Conventional Commits.
- Never write or edit a pull request description. Open PRs with an empty body.
- Fixture repositories: `mptooling/notifycat-it-alpha`, `mptooling/notifycat-it-mono`, `mptooling/notifycat-it-nohook`.
- Slack channel roles: `primary`, `secondary`, `team-a`, `team-b`, `alerts`.
- Run marker format: `[it-<run id>]`, embedded in every fixture pull-request title.
- Suite wall-clock budget: 15 minutes.
- Working directory for tasks 1–12 is `/Users/pavlomaksymov/projects/notifycat-integration-tests`. Task 13 is in `/Users/pavlomaksymov/projects/notifycat`.

---

### Task 1: Bootstrap the integration repository

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `justfile`
- Create: `README.md`
- Create: `CLAUDE.md`
- Create: `.github/workflows/lint.yml`

**Interfaces:**
- Consumes: nothing.
- Produces: module path `github.com/mptooling/notifycat-integration-tests`, used as the import prefix by every later task.

- [ ] **Step 1: Clone the repository**

```bash
cd /Users/pavlomaksymov/projects
gh repo clone mptooling/notifycat-integration-tests
cd notifycat-integration-tests
```

- [ ] **Step 2: Initialise the Go module**

```bash
go mod init github.com/mptooling/notifycat-integration-tests
go get github.com/stretchr/testify@latest
```

- [ ] **Step 3: Write `.gitignore`**

```gitignore
# Rendered per run from config/config.yaml.tmpl — contains real channel IDs.
/config/config.yaml
/config/*.lock
/tmp/
*.log
```

- [ ] **Step 4: Write `justfile`**

```just
# Unit tests for the harness packages. No network, no Docker.
test-unit:
    go test ./internal/...

# Full integration suite. Requires the secrets in README.md.
test:
    go test -tags=integration -timeout=15m ./tests/...

# Validation scenarios only — no tunnel, no Slack delivery.
test-validation:
    go test -tags=integration -timeout=5m -run TestValidation ./tests/...

lint:
    go vet ./...
    golangci-lint run
```

- [ ] **Step 5: Write `CLAUDE.md`**

````markdown
# CLAUDE.md

Integration test suite for [notifycat](https://github.com/mptooling/notifycat).
It drives real GitHub fixture repositories through a real webhook delivery into
the published container image and asserts the result in a real Slack workspace.

## Layout

- `internal/testenv` — environment loading, the run marker, secret-based skips.
- `internal/slackx` — the only code that calls the Slack API.
- `internal/githubx` — the only code that calls the GitHub API.
- `internal/harness` — container lifecycle, tunnel lifecycle, config rendering.
- `tests/` — scenarios. Nothing here builds an HTTP request directly.

## Rules

- Every test uses testify. `require` when a failure makes the rest meaningless,
  `assert` for independent end-of-test checks. Argument order `(t, want, got)`.
- Table tests: `testCases := []struct{ name string; … }` through
  `t.Run(testCase.name, …)`. Spell loop variables out — `testCase`, never `tc`.
- Readable names over terse Go idiom. `repoFullName`, not `r`.
- Comment only a non-obvious *why*. Never restate the code.
- More than three arguments to an exported function → one input struct.
- Every Slack assertion goes through `require.Eventually`. Never `time.Sleep`.
- Scenario files carry the `integration` build tag; `internal/` unit tests do not.
- Conventional Commits. Never mention AI authorship in a commit message.

## Commands

| Task | Command |
| --- | --- |
| Harness unit tests (no network) | `just test-unit` |
| Full suite | `just test` |
| Validation scenarios only | `just test-validation` |
| Lint | `just lint` |
````

- [ ] **Step 6: Write `README.md`**

````markdown
# notifycat-integration-tests

Integration suite for [notifycat](https://github.com/mptooling/notifycat).

It creates a webhook on a fixture repository pointing at a
[cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)
quick tunnel, acts on that repository through the GitHub API, and asserts what
notifycat posted to a real Slack workspace.

Design: `docs/superpowers/specs/2026-09-06-integration-testing-design.md` in the
notifycat repository.

## Running locally

Requires Docker, `cloudflared` on `PATH`, and the secrets below exported.

```sh
just test-unit        # harness unit tests, no network
just test-validation  # validation scenarios, no tunnel needed
just test             # everything
```

## Required secrets

Every scenario declares what it needs and skips with a named reason when a
value is absent, so a partially configured run is still green and honest.

| Variable | Needed by | How to get it |
| --- | --- | --- |
| `SLACK_BOT_TOKEN` | everything | Slack app → OAuth & Permissions → Bot User OAuth Token (`xoxb-`) |
| `SLACK_CHANNEL_PRIMARY` | everything | Channel → View channel details → ID at the bottom |
| `SLACK_CHANNEL_SECONDARY` | fan-out scenarios | as above |
| `SLACK_CHANNEL_TEAM_A` | path routing | as above |
| `SLACK_CHANNEL_TEAM_B` | path routing | as above |
| `SLACK_CHANNEL_ALERTS` | CI failure reporting only | as above |
| `GITHUB_AUTHOR_TOKEN` | everything | Fine-grained PAT, the three fixture repos only: contents write, pull requests write, webhooks admin |
| `REVIEWER_APP_ID` | approval scenarios | GitHub App → App ID |
| `REVIEWER_APP_PRIVATE_KEY` | approval scenarios | GitHub App → generated private key, PEM contents |
| `NOTIFYCAT_WEBHOOK_SECRET` | everything | Any random string; used both when creating webhooks and as the container's `GITHUB_WEBHOOK_SECRET` |
| `NOTIFYCAT_IMAGE_TAG` | optional | Image tag under test. Default `edge` |

Slack bot scopes: `chat:write`, `channels:read`, `channels:history`,
`reactions:read`, `reactions:write`. Invite the bot to all five channels.

## Manual setup checklist

1. Slack app in the test workspace with the scopes above; install; copy the token.
2. Create the five channels; invite the bot to each; record the IDs.
3. Fine-grained PAT (author) scoped to the three fixture repositories.
4. GitHub App (reviewer); install it on the three fixture repositories.
5. Fine-grained PAT with Actions: write on this repository only; add it to the
   **notifycat** repository as `INTEGRATION_DISPATCH_TOKEN`.
6. Add every variable above to this repository's Actions secrets.
````

- [ ] **Step 7: Write `.github/workflows/lint.yml`**

```yaml
name: lint

on:
  push:
    branches:
      - main
  pull_request:

permissions:
  contents: read

env:
  GO_VERSION: "1.25.14"

jobs:
  lint:
    name: Vet and unit tests
    runs-on: ubuntu-latest

    steps:
      - name: Check out repository
        uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v7
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true

      - name: Vet
        run: go vet ./...

      - name: Unit tests
        run: go test ./internal/...
```

- [ ] **Step 8: Verify it builds and commit**

```bash
go build ./... && go vet ./...
git add -A
git commit -m "chore: bootstrap the integration test module"
git push -u origin main
```

Expected: `go vet` silent, push succeeds.

---

### Task 2: Create and seed the fixture repositories

**Files:**
- Create: `scripts/create-fixtures.sh`

**Interfaces:**
- Consumes: nothing.
- Produces: three public repositories — `mptooling/notifycat-it-alpha`, `mptooling/notifycat-it-mono`, `mptooling/notifycat-it-nohook` — each with `main` as the default branch and the tracked files listed below. Every later task assumes those paths exist.

The `mono` tree exists so path rules have something to match. Each directory holds one tracked file, because a PR must be able to touch exactly one directory.

- [ ] **Step 1: Write `scripts/create-fixtures.sh`**

```bash
#!/usr/bin/env bash
# Creates and seeds the three GitHub fixture repositories the suite acts on.
# Idempotent: a repository that already exists is left alone.
set -euo pipefail

owner="mptooling"
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

create_repo() {
  local name="$1" description="$2"
  if gh repo view "${owner}/${name}" >/dev/null 2>&1; then
    echo "${owner}/${name} already exists — skipping"
    return 1
  fi
  gh repo create "${owner}/${name}" --public --description "$description"
  return 0
}

seed() {
  local name="$1"; shift
  local dir="${workdir}/${name}"
  git init -q "$dir"
  git -C "$dir" symbolic-ref HEAD refs/heads/main
  for path in "$@"; do
    mkdir -p "$dir/$(dirname "$path")"
    printf 'Fixture file for the notifycat integration suite.\n' > "$dir/$path"
  done
  git -C "$dir" add -A
  git -C "$dir" -c user.email=fixtures@notifycat.invalid -c user.name=notifycat-fixtures \
    commit -qm "chore: seed fixture contents"
  git -C "$dir" remote add origin "https://github.com/${owner}/${name}.git"
  git -C "$dir" push -q -u origin main
  echo "seeded ${owner}/${name}"
}

if create_repo notifycat-it-alpha "Fixture: flat repository for the notifycat integration suite"; then
  seed notifycat-it-alpha README.md src/app.txt
fi

if create_repo notifycat-it-mono "Fixture: monorepo layout for the notifycat integration suite"; then
  seed notifycat-it-mono README.md \
    modules/acme/service.txt \
    modules/betta/service.txt \
    config/settings.txt \
    src/AuthBundle/auth.txt \
    vendor/library.txt
fi

if create_repo notifycat-it-nohook "Fixture: never receives a webhook — drives the WARN validation path"; then
  seed notifycat-it-nohook README.md
fi

echo "done"
```

- [ ] **Step 2: Run it**

```bash
chmod +x scripts/create-fixtures.sh
./scripts/create-fixtures.sh
```

Expected: three `seeded mptooling/notifycat-it-*` lines.

- [ ] **Step 3: Verify the seeded trees**

```bash
gh api repos/mptooling/notifycat-it-mono/git/trees/main?recursive=1 --jq '.tree[].path'
```

Expected: includes `modules/acme/service.txt`, `modules/betta/service.txt`, `config/settings.txt`, `src/AuthBundle/auth.txt`, `vendor/library.txt`, `README.md`.

- [ ] **Step 4: Commit**

```bash
git add scripts/create-fixtures.sh
git commit -m "chore: add the fixture repository bootstrap script"
```

---

### Task 3: `internal/testenv` — environment, run marker, skips

**Files:**
- Create: `internal/testenv/env.go`
- Test: `internal/testenv/env_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Channels struct{ Primary, Secondary, TeamA, TeamB, Alerts string }`
  - `type Fixtures struct{ Alpha, Mono, NoHook string }` — `owner/repo` strings
  - `type Env struct{ ImageTag, WebhookSecret, SlackBotToken, GitHubAuthorToken, ReviewerAppID, ReviewerPrivateKey, RunID string; Channels Channels; Fixtures Fixtures }`
  - `func Load() (Env, error)`
  - `func (e Env) Marker() string`
  - `func SkipUnless(t *testing.T, required map[string]string)`

- [ ] **Step 1: Write the failing test**

`internal/testenv/env_test.go`:

```go
package testenv_test

import (
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadReadsTheEnvironment(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("GITHUB_AUTHOR_TOKEN", "ghp-test")
	t.Setenv("NOTIFYCAT_WEBHOOK_SECRET", "s3cret")
	t.Setenv("SLACK_CHANNEL_PRIMARY", "C0PRIMARY00")
	t.Setenv("GITHUB_RUN_ID", "12345")

	env, err := testenv.Load()

	require.NoError(t, err)
	assert.Equal(t, "xoxb-test", env.SlackBotToken)
	assert.Equal(t, "C0PRIMARY00", env.Channels.Primary)
	assert.Equal(t, "mptooling/notifycat-it-alpha", env.Fixtures.Alpha)
}

func TestLoadDefaultsTheImageTagToEdge(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("GITHUB_AUTHOR_TOKEN", "ghp-test")
	t.Setenv("NOTIFYCAT_WEBHOOK_SECRET", "s3cret")

	env, err := testenv.Load()

	require.NoError(t, err)
	assert.Equal(t, "edge", env.ImageTag)
}

func TestLoadFailsWithoutTheAlwaysRequiredValues(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "")
	t.Setenv("GITHUB_AUTHOR_TOKEN", "")
	t.Setenv("NOTIFYCAT_WEBHOOK_SECRET", "")

	_, err := testenv.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SLACK_BOT_TOKEN")
}

func TestMarkerIsUniquePerRun(t *testing.T) {
	env := testenv.Env{RunID: "987"}

	assert.Equal(t, "[it-987]", env.Marker())
}

func TestLoadGeneratesARunIDWhenTheEnvironmentHasNone(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("GITHUB_AUTHOR_TOKEN", "ghp-test")
	t.Setenv("NOTIFYCAT_WEBHOOK_SECRET", "s3cret")
	t.Setenv("GITHUB_RUN_ID", "")

	env, err := testenv.Load()

	require.NoError(t, err)
	assert.NotEmpty(t, env.RunID, "a local run still needs a marker that cannot collide")
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/testenv/...`
Expected: FAIL — package `testenv` does not exist.

- [ ] **Step 3: Write `internal/testenv/env.go`**

```go
// Package testenv loads every value the integration suite reads from the
// environment and turns a missing optional secret into a named skip rather
// than an opaque failure.
package testenv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// Channels holds the Slack channel IDs the fixtures route to. Alerts is never
// a notification target — CI posts suite failures there.
type Channels struct {
	Primary   string
	Secondary string
	TeamA     string
	TeamB     string
	Alerts    string
}

// Fixtures holds the owner/repo of each GitHub fixture repository. They are
// fixed infrastructure, not configuration, so they are not read from the
// environment.
type Fixtures struct {
	Alpha  string
	Mono   string
	NoHook string
}

// Env is the fully resolved environment for one suite run.
type Env struct {
	ImageTag           string
	WebhookSecret      string
	SlackBotToken      string
	GitHubAuthorToken  string
	ReviewerAppID      string
	ReviewerPrivateKey string
	RunID              string
	Channels           Channels
	Fixtures           Fixtures
}

const defaultImageTag = "edge"

// Load reads the environment. It fails only on the values every scenario
// needs; optional ones are left empty for SkipUnless to report.
func Load() (Env, error) {
	env := Env{
		ImageTag:           valueOr(os.Getenv("NOTIFYCAT_IMAGE_TAG"), defaultImageTag),
		WebhookSecret:      os.Getenv("NOTIFYCAT_WEBHOOK_SECRET"),
		SlackBotToken:      os.Getenv("SLACK_BOT_TOKEN"),
		GitHubAuthorToken:  os.Getenv("GITHUB_AUTHOR_TOKEN"),
		ReviewerAppID:      os.Getenv("REVIEWER_APP_ID"),
		ReviewerPrivateKey: os.Getenv("REVIEWER_APP_PRIVATE_KEY"),
		RunID:              valueOr(os.Getenv("GITHUB_RUN_ID"), randomRunID()),
		Channels: Channels{
			Primary:   os.Getenv("SLACK_CHANNEL_PRIMARY"),
			Secondary: os.Getenv("SLACK_CHANNEL_SECONDARY"),
			TeamA:     os.Getenv("SLACK_CHANNEL_TEAM_A"),
			TeamB:     os.Getenv("SLACK_CHANNEL_TEAM_B"),
			Alerts:    os.Getenv("SLACK_CHANNEL_ALERTS"),
		},
		Fixtures: Fixtures{
			Alpha:  "mptooling/notifycat-it-alpha",
			Mono:   "mptooling/notifycat-it-mono",
			NoHook: "mptooling/notifycat-it-nohook",
		},
	}

	var missing []string
	for name, value := range map[string]string{
		"SLACK_BOT_TOKEN":          env.SlackBotToken,
		"GITHUB_AUTHOR_TOKEN":      env.GitHubAuthorToken,
		"NOTIFYCAT_WEBHOOK_SECRET": env.WebhookSecret,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Env{}, fmt.Errorf("missing required environment: %s", strings.Join(missing, ", "))
	}
	return env, nil
}

// Marker is the per-run string embedded in every fixture pull-request title and
// therefore in every Slack message the run produces. Assertions filter on it so
// a message left behind by an earlier run can never satisfy them.
func (e Env) Marker() string {
	return "[it-" + e.RunID + "]"
}

// SkipUnless skips the test when any required value is empty, naming the
// variables that were missing so a skipped run explains itself.
func SkipUnless(t *testing.T, required map[string]string) {
	t.Helper()

	var missing []string
	for name, value := range required {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	t.Skipf("not configured: %s", strings.Join(missing, ", "))
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func randomRunID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "local"
	}
	return "local" + hex.EncodeToString(buf)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/testenv/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/testenv
git commit -m "feat: load the suite environment and derive the run marker"
```

---

### Task 4: `internal/slackx` — Slack read and cleanup client

**Files:**
- Create: `internal/slackx/client.go`
- Test: `internal/slackx/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Message struct{ Timestamp, Text string; Reactions []string }`
  - `type ClientConfig struct{ BaseURL, Token string; HTTPClient *http.Client }`
  - `func NewClient(cfg ClientConfig) *Client`
  - `func (c *Client) History(ctx context.Context, channelID string) ([]Message, error)`
  - `func (c *Client) FindByMarker(ctx context.Context, channelID, marker string) (Message, bool, error)`
  - `func (c *Client) Reactions(ctx context.Context, channelID, timestamp string) ([]string, error)`
  - `func (c *Client) Delete(ctx context.Context, channelID, timestamp string) error`

- [ ] **Step 1: Write the failing test**

`internal/slackx/client_test.go`:

```go
package slackx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/slackx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *slackx.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return slackx.NewClient(slackx.ClientConfig{BaseURL: server.URL, Token: "xoxb-test"})
}

func TestHistoryReturnsMessagesWithReactions(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/conversations.history", r.URL.Path)
		assert.Equal(t, "Bearer xoxb-test", r.Header.Get("Authorization"))
		assert.Equal(t, "C0PRIMARY00", r.URL.Query().Get("channel"))
		_, _ = w.Write([]byte(`{"ok":true,"messages":[
			{"ts":"1.1","text":"new PR [it-1]","reactions":[{"name":"eyes"}]},
			{"ts":"2.2","text":"other"}
		]}`))
	})

	messages, err := client.History(context.Background(), "C0PRIMARY00")

	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, "1.1", messages[0].Timestamp)
	assert.Equal(t, []string{"eyes"}, messages[0].Reactions)
	assert.Empty(t, messages[1].Reactions)
}

func TestHistoryTurnsASlackErrorIntoAnError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	})

	_, err := client.History(context.Background(), "C0MISSING00")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel_not_found")
}

func TestFindByMarkerMatchesOnlyTheRunsOwnMessage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"messages":[
			{"ts":"9.9","text":"stale [it-000]"},
			{"ts":"1.1","text":"fresh [it-777]"}
		]}`))
	})

	message, found, err := client.FindByMarker(context.Background(), "C0PRIMARY00", "[it-777]")

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "1.1", message.Timestamp)
}

func TestFindByMarkerReportsNotFoundRatherThanFailing(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"messages":[]}`))
	})

	_, found, err := client.FindByMarker(context.Background(), "C0PRIMARY00", "[it-777]")

	require.NoError(t, err)
	assert.False(t, found)
}

func TestReactionsReadsTheLiveReactionList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/reactions.get", r.URL.Path)
		assert.Equal(t, "1.1", r.URL.Query().Get("timestamp"))
		_, _ = w.Write([]byte(`{"ok":true,"message":{"reactions":[{"name":"eyes"},{"name":"white_check_mark"}]}}`))
	})

	names, err := client.Reactions(context.Background(), "C0PRIMARY00", "1.1")

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"eyes", "white_check_mark"}, names)
}

func TestDeletePostsTheTimestamp(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/chat.delete", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "1.1", r.PostForm.Get("ts"))
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	err := client.Delete(context.Background(), "C0PRIMARY00", "1.1")

	require.NoError(t, err)
}

func TestRetriesOnceOnRateLimit(t *testing.T) {
	attempts := 0
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"messages":[]}`))
	})

	_, err := client.History(context.Background(), "C0PRIMARY00")

	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/slackx/...`
Expected: FAIL — package `slackx` does not exist.

- [ ] **Step 3: Write `internal/slackx/client.go`**

```go
// Package slackx is the only code in the suite that calls the Slack API. It
// returns plain structs and never asserts, so scenarios stay readable.
package slackx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Message is one Slack message reduced to what the suite asserts on.
type Message struct {
	Timestamp string
	Text      string
	Reactions []string
}

// ClientConfig configures NewClient. BaseURL exists so tests can point the
// client at an httptest server; production callers pass https://slack.com.
type ClientConfig struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// Client reads Slack conversation state and deletes the suite's own messages.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimSuffix(cfg.BaseURL, "/"),
		token:      cfg.Token,
		httpClient: httpClient,
	}
}

type reaction struct {
	Name string `json:"name"`
}

type apiMessage struct {
	Timestamp string     `json:"ts"`
	Text      string     `json:"text"`
	Reactions []reaction `json:"reactions"`
}

type apiResponse struct {
	OK       bool         `json:"ok"`
	Error    string       `json:"error"`
	Messages []apiMessage `json:"messages"`
	Message  apiMessage   `json:"message"`
}

// History returns the channel's most recent messages, newest first.
func (c *Client) History(ctx context.Context, channelID string) ([]Message, error) {
	query := url.Values{"channel": {channelID}, "limit": {"100"}}

	response, err := c.get(ctx, "conversations.history", query)
	if err != nil {
		return nil, err
	}

	messages := make([]Message, 0, len(response.Messages))
	for _, raw := range response.Messages {
		messages = append(messages, toMessage(raw))
	}
	return messages, nil
}

// FindByMarker returns the channel's message carrying the run marker. A run
// posts at most one message per channel per pull request, so the first match
// is the only match.
func (c *Client) FindByMarker(ctx context.Context, channelID, marker string) (Message, bool, error) {
	messages, err := c.History(ctx, channelID)
	if err != nil {
		return Message{}, false, err
	}
	for _, message := range messages {
		if strings.Contains(message.Text, marker) {
			return message, true, nil
		}
	}
	return Message{}, false, nil
}

// Reactions reads the live reaction list for one message. conversations.history
// can lag behind reactions.add, so assertions on emoji use this instead.
func (c *Client) Reactions(ctx context.Context, channelID, timestamp string) ([]string, error) {
	query := url.Values{"channel": {channelID}, "timestamp": {timestamp}}

	response, err := c.get(ctx, "reactions.get", query)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(response.Message.Reactions))
	for _, item := range response.Message.Reactions {
		names = append(names, item.Name)
	}
	return names, nil
}

// Delete removes a message the bot itself posted.
func (c *Client) Delete(ctx context.Context, channelID, timestamp string) error {
	form := url.Values{"channel": {channelID}, "ts": {timestamp}}

	_, err := c.post(ctx, "chat.delete", form)
	return err
}

func (c *Client) get(ctx context.Context, method string, query url.Values) (apiResponse, error) {
	endpoint := c.baseURL + "/api/" + method + "?" + query.Encode()

	return c.do(ctx, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	}, method)
}

func (c *Client) post(ctx context.Context, method string, form url.Values) (apiResponse, error) {
	endpoint := c.baseURL + "/api/" + method

	return c.do(ctx, func() (*http.Request, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return request, nil
	}, method)
}

// do sends the request, retrying once on HTTP 429. Slack's Tier 3 limits are
// far above this suite's call volume, so a single retry is defence in depth
// rather than a load-shedding strategy.
func (c *Client) do(ctx context.Context, build func() (*http.Request, error), method string) (apiResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		request, err := build()
		if err != nil {
			return apiResponse{}, fmt.Errorf("slack %s: build request: %w", method, err)
		}
		request.Header.Set("Authorization", "Bearer "+c.token)

		response, err := c.httpClient.Do(request)
		if err != nil {
			return apiResponse{}, fmt.Errorf("slack %s: %w", method, err)
		}

		if response.StatusCode == http.StatusTooManyRequests {
			wait := retryAfter(response.Header.Get("Retry-After"))
			_ = response.Body.Close()
			select {
			case <-ctx.Done():
				return apiResponse{}, ctx.Err()
			case <-time.After(wait):
			}
			continue
		}

		var decoded apiResponse
		decodeErr := json.NewDecoder(response.Body).Decode(&decoded)
		_ = response.Body.Close()
		if decodeErr != nil {
			return apiResponse{}, fmt.Errorf("slack %s: decode: %w", method, decodeErr)
		}
		if !decoded.OK {
			return apiResponse{}, fmt.Errorf("slack %s: %s", method, decoded.Error)
		}
		return decoded, nil
	}
	return apiResponse{}, fmt.Errorf("slack %s: rate limited twice", method)
}

func retryAfter(header string) time.Duration {
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds < 0 {
		return time.Second
	}
	return time.Duration(seconds) * time.Second
}

func toMessage(raw apiMessage) Message {
	names := make([]string, 0, len(raw.Reactions))
	for _, item := range raw.Reactions {
		names = append(names, item.Name)
	}
	return Message{Timestamp: raw.Timestamp, Text: raw.Text, Reactions: names}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/slackx/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/slackx
git commit -m "feat: add the Slack read and cleanup client"
```

---

### Task 5: `internal/githubx` — repository actions and webhook lifecycle

**Files:**
- Create: `internal/githubx/client.go`
- Test: `internal/githubx/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type PullRequest struct{ Number int; HTMLURL, HeadRef string }`
  - `type Webhook struct{ ID int64; URL string }`
  - `type ClientConfig struct{ BaseURL, Token string; HTTPClient *http.Client }`
  - `func NewClient(cfg ClientConfig) *Client`
  - `type CommitFilesRequest struct{ Repo, Branch, Message string; Files map[string]string }`
  - `func (c *Client) CommitFilesOnNewBranch(ctx context.Context, req CommitFilesRequest) error`
  - `type OpenPullRequestRequest struct{ Repo, Head, Base, Title, Body string }`
  - `func (c *Client) OpenPullRequest(ctx context.Context, req OpenPullRequestRequest) (PullRequest, error)`
  - `type ReviewRequest struct{ Repo string; Number int; Event, Body string }`
  - `func (c *Client) SubmitReview(ctx context.Context, req ReviewRequest) error`
  - `func (c *Client) MergePullRequest(ctx context.Context, repo string, number int) error`
  - `func (c *Client) ClosePullRequest(ctx context.Context, repo string, number int) error`
  - `func (c *Client) DeleteBranch(ctx context.Context, repo, branch string) error`
  - `type CreateWebhookRequest struct{ Repo, URL, Secret string; Events []string }`
  - `func (c *Client) CreateWebhook(ctx context.Context, req CreateWebhookRequest) (Webhook, error)`
  - `func (c *Client) ListWebhooks(ctx context.Context, repo string) ([]Webhook, error)`
  - `func (c *Client) DeleteWebhook(ctx context.Context, repo string, id int64) error`

`CommitFilesOnNewBranch` is three GitHub calls: read `main`'s SHA, create the ref, then `PUT` each file's contents on that branch. The contents API cannot create a branch, which is why the ref comes first.

- [ ] **Step 1: Write the failing test**

`internal/githubx/client_test.go`:

```go
package githubx_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/githubx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *githubx.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return githubx.NewClient(githubx.ClientConfig{BaseURL: server.URL, Token: "ghp-test"})
}

func TestCommitFilesOnNewBranchCreatesTheRefThenTheFiles(t *testing.T) {
	var visited []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		visited = append(visited, r.Method+" "+r.URL.Path)
		assert.Equal(t, "Bearer ghp-test", r.Header.Get("Authorization"))

		switch {
		case r.URL.Path == "/repos/acme/api/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"basesha"}}`))
		case r.URL.Path == "/repos/acme/api/git/refs":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			require.NoError(t, json.Unmarshal(body, &payload))
			assert.Equal(t, "refs/heads/it-1", payload["ref"])
			assert.Equal(t, "basesha", payload["sha"])
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		}
	})

	err := client.CommitFilesOnNewBranch(context.Background(), githubx.CommitFilesRequest{
		Repo:    "acme/api",
		Branch:  "it-1",
		Message: "test change",
		Files:   map[string]string{"src/app.txt": "changed"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{
		"GET /repos/acme/api/git/ref/heads/main",
		"POST /repos/acme/api/git/refs",
		"PUT /repos/acme/api/contents/src/app.txt",
	}, visited)
}

func TestOpenPullRequestReturnsTheNumberAndURL(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/acme/api/pulls", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42,"html_url":"https://github.com/acme/api/pull/42","head":{"ref":"it-1"}}`))
	})

	pullRequest, err := client.OpenPullRequest(context.Background(), githubx.OpenPullRequestRequest{
		Repo: "acme/api", Head: "it-1", Base: "main", Title: "test [it-1]",
	})

	require.NoError(t, err)
	assert.Equal(t, 42, pullRequest.Number)
	assert.Equal(t, "it-1", pullRequest.HeadRef)
}

func TestSubmitReviewSendsTheEvent(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/acme/api/pulls/42/reviews", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, "COMMENT", payload["event"])
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	err := client.SubmitReview(context.Background(), githubx.ReviewRequest{
		Repo: "acme/api", Number: 42, Event: "COMMENT", Body: "looking",
	})

	require.NoError(t, err)
}

func TestMergePullRequestUsesThePutEndpoint(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/repos/acme/api/pulls/42/merge", r.URL.Path)
		_, _ = w.Write([]byte(`{"merged":true}`))
	})

	err := client.MergePullRequest(context.Background(), "acme/api", 42)

	require.NoError(t, err)
}

func TestCreateWebhookReturnsTheID(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/acme/api/hooks", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":99,"config":{"url":"https://x.trycloudflare.com/webhook/github"}}`))
	})

	webhook, err := client.CreateWebhook(context.Background(), githubx.CreateWebhookRequest{
		Repo: "acme/api", URL: "https://x.trycloudflare.com/webhook/github", Secret: "s3cret",
	})

	require.NoError(t, err)
	assert.Equal(t, int64(99), webhook.ID)
}

func TestDeleteWebhookToleratesAnAlreadyDeletedHook(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	err := client.DeleteWebhook(context.Background(), "acme/api", 99)

	require.NoError(t, err, "cleanup must be idempotent — a hook deleted twice is not a failure")
}

func TestAnUnexpectedStatusBecomesAnErrorCarryingTheBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))
	})

	_, err := client.OpenPullRequest(context.Background(), githubx.OpenPullRequestRequest{Repo: "acme/api"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Reference already exists")
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/githubx/...`
Expected: FAIL — package `githubx` does not exist.

- [ ] **Step 3: Write `internal/githubx/client.go`**

```go
// Package githubx is the only code in the suite that calls the GitHub API. It
// performs repository actions and manages the per-scenario webhooks; it never
// asserts.
package githubx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// PullRequest is a fixture pull request reduced to what scenarios need.
type PullRequest struct {
	Number  int
	HTMLURL string
	HeadRef string
}

// Webhook is one repository webhook.
type Webhook struct {
	ID  int64
	URL string
}

// ClientConfig configures NewClient. BaseURL lets tests point at an httptest
// server; production callers pass https://api.github.com.
type ClientConfig struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// Client performs fixture repository actions.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimSuffix(cfg.BaseURL, "/"),
		token:      cfg.Token,
		httpClient: httpClient,
	}
}

// CommitFilesRequest describes one branch carrying one commit per file.
type CommitFilesRequest struct {
	Repo    string
	Branch  string
	Message string
	Files   map[string]string
}

// CommitFilesOnNewBranch branches off main and writes each file on it. The
// contents API cannot create a branch, so the ref is created first.
func (c *Client) CommitFilesOnNewBranch(ctx context.Context, req CommitFilesRequest) error {
	var base struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.call(ctx, http.MethodGet, "/repos/"+req.Repo+"/git/ref/heads/main", nil, &base); err != nil {
		return fmt.Errorf("read main ref: %w", err)
	}

	newRef := map[string]string{"ref": "refs/heads/" + req.Branch, "sha": base.Object.SHA}
	if err := c.call(ctx, http.MethodPost, "/repos/"+req.Repo+"/git/refs", newRef, nil); err != nil {
		return fmt.Errorf("create branch %s: %w", req.Branch, err)
	}

	// Sorted so the request sequence is deterministic and assertable.
	paths := make([]string, 0, len(req.Files))
	for path := range req.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		payload := map[string]string{
			"message": req.Message,
			"content": base64.StdEncoding.EncodeToString([]byte(req.Files[path])),
			"branch":  req.Branch,
		}
		if sha, err := c.fileSHA(ctx, req.Repo, path, req.Branch); err == nil && sha != "" {
			payload["sha"] = sha
		}
		if err := c.call(ctx, http.MethodPut, "/repos/"+req.Repo+"/contents/"+path, payload, nil); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// OpenPullRequestRequest describes the pull request to open.
type OpenPullRequestRequest struct {
	Repo  string
	Head  string
	Base  string
	Title string
	Body  string
}

func (c *Client) OpenPullRequest(ctx context.Context, req OpenPullRequestRequest) (PullRequest, error) {
	payload := map[string]string{"head": req.Head, "base": req.Base, "title": req.Title, "body": req.Body}

	var decoded struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := c.call(ctx, http.MethodPost, "/repos/"+req.Repo+"/pulls", payload, &decoded); err != nil {
		return PullRequest{}, err
	}
	return PullRequest{Number: decoded.Number, HTMLURL: decoded.HTMLURL, HeadRef: decoded.Head.Ref}, nil
}

// ReviewRequest describes a review to submit. Event is COMMENT, APPROVE or
// REQUEST_CHANGES; GitHub rejects the latter two from a pull request's author.
type ReviewRequest struct {
	Repo   string
	Number int
	Event  string
	Body   string
}

func (c *Client) SubmitReview(ctx context.Context, req ReviewRequest) error {
	payload := map[string]string{"event": req.Event, "body": req.Body}
	path := fmt.Sprintf("/repos/%s/pulls/%d/reviews", req.Repo, req.Number)

	return c.call(ctx, http.MethodPost, path, payload, nil)
}

func (c *Client) MergePullRequest(ctx context.Context, repo string, number int) error {
	path := fmt.Sprintf("/repos/%s/pulls/%d/merge", repo, number)

	return c.call(ctx, http.MethodPut, path, map[string]string{"merge_method": "squash"}, nil)
}

func (c *Client) ClosePullRequest(ctx context.Context, repo string, number int) error {
	path := fmt.Sprintf("/repos/%s/pulls/%d", repo, number)

	return c.call(ctx, http.MethodPatch, path, map[string]string{"state": "closed"}, nil)
}

func (c *Client) DeleteBranch(ctx context.Context, repo, branch string) error {
	return c.call(ctx, http.MethodDelete, "/repos/"+repo+"/git/refs/heads/"+branch, nil, nil)
}

// CreateWebhookRequest describes a webhook. Events defaults to the three
// notifycat subscribes to.
type CreateWebhookRequest struct {
	Repo   string
	URL    string
	Secret string
	Events []string
}

func (c *Client) CreateWebhook(ctx context.Context, req CreateWebhookRequest) (Webhook, error) {
	events := req.Events
	if len(events) == 0 {
		events = []string{"pull_request", "pull_request_review", "pull_request_review_comment"}
	}
	payload := map[string]any{
		"name":   "web",
		"active": true,
		"events": events,
		"config": map[string]string{
			"url":          req.URL,
			"content_type": "json",
			"secret":       req.Secret,
			"insecure_ssl": "0",
		},
	}

	var decoded struct {
		ID     int64 `json:"id"`
		Config struct {
			URL string `json:"url"`
		} `json:"config"`
	}
	if err := c.call(ctx, http.MethodPost, "/repos/"+req.Repo+"/hooks", payload, &decoded); err != nil {
		return Webhook{}, err
	}
	return Webhook{ID: decoded.ID, URL: decoded.Config.URL}, nil
}

func (c *Client) ListWebhooks(ctx context.Context, repo string) ([]Webhook, error) {
	var decoded []struct {
		ID     int64 `json:"id"`
		Config struct {
			URL string `json:"url"`
		} `json:"config"`
	}
	if err := c.call(ctx, http.MethodGet, "/repos/"+repo+"/hooks", nil, &decoded); err != nil {
		return nil, err
	}

	hooks := make([]Webhook, 0, len(decoded))
	for _, item := range decoded {
		hooks = append(hooks, Webhook{ID: item.ID, URL: item.Config.URL})
	}
	return hooks, nil
}

// DeleteWebhook removes a hook. A hook that is already gone is not an error —
// teardown runs after failures and must be idempotent.
func (c *Client) DeleteWebhook(ctx context.Context, repo string, id int64) error {
	path := fmt.Sprintf("/repos/%s/hooks/%d", repo, id)

	err := c.call(ctx, http.MethodDelete, path, nil, nil)
	var status *StatusError
	if errors.As(err, &status) && status.Code == http.StatusNotFound {
		return nil
	}
	return err
}

// StatusError is an unexpected HTTP status carrying GitHub's response body,
// which is where the actionable message lives.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("github: unexpected status %d: %s", e.Code, e.Body)
}

func (c *Client) fileSHA(ctx context.Context, repo, path, branch string) (string, error) {
	var decoded struct {
		SHA string `json:"sha"`
	}
	err := c.call(ctx, http.MethodGet, "/repos/"+repo+"/contents/"+path+"?ref="+branch, nil, &decoded)
	return decoded.SHA, err
}

func (c *Client) call(ctx context.Context, method, path string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("github: encode payload: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("github: build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("github %s %s: %w", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("github %s %s: read body: %w", method, path, err)
	}
	if response.StatusCode >= 300 {
		return &StatusError{Code: response.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("github %s %s: decode: %w", method, path, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/githubx/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/githubx
git commit -m "feat: add the GitHub fixture and webhook client"
```

---

### Task 6: `internal/harness` — config rendering

**Files:**
- Create: `config/config.yaml.tmpl`
- Create: `internal/harness/config.go`
- Test: `internal/harness/config_test.go`

**Interfaces:**
- Consumes: `testenv.Channels`, `testenv.Fixtures` from Task 3.
- Produces:
  - `type ConfigInput struct{ Channels testenv.Channels; Fixtures testenv.Fixtures; BadChannel bool }`
  - `func RenderConfig(templatePath string, input ConfigInput) ([]byte, error)`
  - `func WriteConfig(dir, templatePath string, input ConfigInput) (string, error)` — writes `config.yaml` into `dir` and returns its path

`BadChannel` swaps the alpha mapping's channel for a nonexistent ID. That is the only way to make the startup gate `FAIL`, which Task 9 asserts.

The digest is disabled in the template. A digest tick firing mid-run would post messages the scenarios did not cause.

- [ ] **Step 1: Write `config/config.yaml.tmpl`**

```yaml
# Rendered per run by internal/harness. Never commit the rendered result —
# it carries real Slack channel IDs.
git_provider: github

server:
  addr: ":8080"
  log_level: debug
  log_format: json
  domain: integration.invalid

database:
  url: "file:/app/data/notifycat.db"

slack:
  base_url: "https://slack.com"
  reactions:
    enabled: true
    new_pr: eyes
    merged_pr: twisted_rightwards_arrows
    closed_pr: x
    approved: white_check_mark
    commented: speech_balloon
    request_change: exclamation
    bot_review: robot_face

github:
  base_url: "https://api.github.com"

cleanup:
  message_ttl_days: 30

reviews:
  ignore_ai_reviews: false
  dependabot_format: true

# A digest tick during a run would post messages no scenario caused.
digest:
  enabled: false

mappings:
  mptooling:
    notifycat-it-alpha:
      channels:
        - channel: {{ .AlphaPrimary }}
        - channel: {{ .Secondary }}
          mentions: []
    notifycat-it-mono:
      channel: {{ .Primary }}
      mentions: []
      paths:
        "/modules/acme":
          channel: {{ .TeamA }}
          mentions: []
        "/modules/betta":
          channel: {{ .TeamB }}
          mentions: []
    notifycat-it-nohook:
      channel: {{ .Primary }}
      mentions: []
```

`notifycat-it-alpha`'s first channel entry omits `mentions`, so it inherits the `<!channel>` fallback; the second sets `mentions: []` and posts silently. Task 11's fan-out scenario asserts exactly that difference.

- [ ] **Step 2: Write the failing test**

`internal/harness/config_test.go`:

```go
package harness_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/harness"
	"github.com/mptooling/notifycat-integration-tests/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testChannels() testenv.Channels {
	return testenv.Channels{
		Primary:   "C0PRIMARY00",
		Secondary: "C0SECOND000",
		TeamA:     "C0TEAMA0000",
		TeamB:     "C0TEAMB0000",
		Alerts:    "C0ALERTS000",
	}
}

func TestRenderConfigSubstitutesEveryChannel(t *testing.T) {
	rendered, err := harness.RenderConfig(filepath.Join("..", "..", "config", "config.yaml.tmpl"),
		harness.ConfigInput{Channels: testChannels()})

	require.NoError(t, err)
	body := string(rendered)
	assert.Contains(t, body, "C0PRIMARY00")
	assert.Contains(t, body, "C0SECOND000")
	assert.Contains(t, body, "C0TEAMA0000")
	assert.Contains(t, body, "C0TEAMB0000")
	assert.NotContains(t, body, "{{", "every placeholder must be substituted")
}

func TestRenderConfigDisablesTheDigest(t *testing.T) {
	rendered, err := harness.RenderConfig(filepath.Join("..", "..", "config", "config.yaml.tmpl"),
		harness.ConfigInput{Channels: testChannels()})

	require.NoError(t, err)
	assert.Contains(t, string(rendered), "enabled: false",
		"a digest tick would post messages no scenario caused")
}

func TestRenderConfigCanPoisonTheAlphaChannel(t *testing.T) {
	rendered, err := harness.RenderConfig(filepath.Join("..", "..", "config", "config.yaml.tmpl"),
		harness.ConfigInput{Channels: testChannels(), BadChannel: true})

	require.NoError(t, err)
	body := string(rendered)
	assert.Contains(t, body, "C0000000BAD")
	assert.NotContains(t, body, "- channel: C0PRIMARY00",
		"the poisoned render must not also route to a real channel")
}

func TestWriteConfigProducesAReadableFile(t *testing.T) {
	dir := t.TempDir()

	path, err := harness.WriteConfig(dir, filepath.Join("..", "..", "config", "config.yaml.tmpl"),
		harness.ConfigInput{Channels: testChannels()})

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "config.yaml"), path)

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "git_provider: github")
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/harness/...`
Expected: FAIL — package `harness` does not exist.

- [ ] **Step 4: Write `internal/harness/config.go`**

```go
// Package harness owns the run environment: it renders notifycat's config,
// starts and stops the container under test, and manages the public tunnel.
// It knows nothing about individual scenarios.
package harness

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/mptooling/notifycat-integration-tests/internal/testenv"
)

// badChannelID is a syntactically valid Slack ID that resolves to nothing, so
// the startup validation gate reports FAIL for the entry that uses it.
const badChannelID = "C0000000BAD"

// ConfigInput is everything config.yaml.tmpl needs. BadChannel poisons the
// alpha mapping so the startup gate fails — the only way to exercise that path.
type ConfigInput struct {
	Channels   testenv.Channels
	Fixtures   testenv.Fixtures
	BadChannel bool
}

type templateData struct {
	AlphaPrimary string
	Primary      string
	Secondary    string
	TeamA        string
	TeamB        string
}

// RenderConfig renders the notifycat configuration for one run.
func RenderConfig(templatePath string, input ConfigInput) ([]byte, error) {
	parsed, err := template.ParseFiles(templatePath)
	if err != nil {
		return nil, fmt.Errorf("parse config template: %w", err)
	}

	alphaPrimary := input.Channels.Primary
	if input.BadChannel {
		alphaPrimary = badChannelID
	}

	data := templateData{
		AlphaPrimary: alphaPrimary,
		Primary:      input.Channels.Primary,
		Secondary:    input.Channels.Secondary,
		TeamA:        input.Channels.TeamA,
		TeamB:        input.Channels.TeamB,
	}

	var out bytes.Buffer
	if err := parsed.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render config template: %w", err)
	}
	return out.Bytes(), nil
}

// WriteConfig renders the template into dir/config.yaml and returns its path.
// The lock file notifycat writes lands beside it, so dir must be writable.
func WriteConfig(dir, templatePath string, input ConfigInput) (string, error) {
	rendered, err := RenderConfig(templatePath, input)
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, rendered, 0o644); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/harness/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add config internal/harness
git commit -m "feat: render the notifycat config for a suite run"
```

---

### Task 7: `internal/harness` — container lifecycle

**Files:**
- Create: `internal/harness/stack.go`
- Test: `internal/harness/stack_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type StackConfig struct{ ImageTag, ConfigPath, StateDir string; HostPort int; Env map[string]string; Command []string }`
  - `func dockerArgs(cfg StackConfig) []string` (unexported, tested through an exported test hook — see below)
  - `type Stack struct{ ... }`
  - `func Start(ctx context.Context, cfg StackConfig) (*Stack, error)`
  - `func (s *Stack) Logs(ctx context.Context) (string, error)`
  - `func (s *Stack) Stop(ctx context.Context) error`
  - `type RunResult struct{ ExitCode int; Output string }`
  - `func RunOnce(ctx context.Context, cfg StackConfig) (RunResult, error)`

`dockerArgs` is the whole testable surface; process management around it is a thin shell. Export it as `DockerArgs` so the test can call it without an internal test file — the suite favours black-box `_test` packages.

The container mounts the rendered `config.yaml` read-only at `/app/config.yaml` and a writable state directory at `/app/data`, matching the compose layout notifycat documents.

- [ ] **Step 1: Write the failing test**

`internal/harness/stack_test.go`:

```go
package harness_test

import (
	"strings"
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/harness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDockerArgsMountsConfigReadOnlyAndStateWritable(t *testing.T) {
	args := harness.DockerArgs(harness.StackConfig{
		ImageTag:   "edge",
		ConfigPath: "/host/config.yaml",
		StateDir:   "/host/state",
		HostPort:   8080,
	})

	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "/host/config.yaml:/app/config.yaml:ro")
	assert.Contains(t, joined, "/host/state:/app/data")
	assert.Contains(t, joined, "-p 127.0.0.1:8080:8080")
	assert.Contains(t, joined, "ghcr.io/mptooling/notifycat:edge")
}

func TestDockerArgsPassesEnvironmentDeterministically(t *testing.T) {
	args := harness.DockerArgs(harness.StackConfig{
		ImageTag: "edge",
		Env:      map[string]string{"SLACK_BOT_TOKEN": "xoxb-t", "GITHUB_WEBHOOK_SECRET": "s3cret"},
	})

	joined := strings.Join(args, " ")
	assert.Contains(t, joined, "-e GITHUB_WEBHOOK_SECRET=s3cret")
	assert.Contains(t, joined, "-e SLACK_BOT_TOKEN=xoxb-t")
	assert.Less(t, strings.Index(joined, "GITHUB_WEBHOOK_SECRET"), strings.Index(joined, "SLACK_BOT_TOKEN"),
		"environment order is sorted so a failing command line is reproducible")
}

func TestDockerArgsAppendsTheCommandAfterTheImage(t *testing.T) {
	args := harness.DockerArgs(harness.StackConfig{
		ImageTag: "edge",
		Command:  []string{"/usr/local/bin/notifycat-config", "validate"},
	})

	require.GreaterOrEqual(t, len(args), 3)
	assert.Equal(t, "validate", args[len(args)-1])
	assert.Equal(t, "/usr/local/bin/notifycat-config", args[len(args)-2])
	assert.Equal(t, "ghcr.io/mptooling/notifycat:edge", args[len(args)-3])
}

func TestDockerArgsOmitsThePortMappingWhenNoPortIsRequested(t *testing.T) {
	args := harness.DockerArgs(harness.StackConfig{ImageTag: "edge"})

	assert.NotContains(t, strings.Join(args, " "), "-p ",
		"a one-shot CLI run must not contend for the host port")
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/harness/...`
Expected: FAIL — `harness.DockerArgs` undefined.

- [ ] **Step 3: Write `internal/harness/stack.go`**

```go
package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const imageRepository = "ghcr.io/mptooling/notifycat"

// StackConfig describes one container run. An empty Command runs the image's
// default, notifycat-server; a non-empty one runs a CLI binary instead.
type StackConfig struct {
	ImageTag   string
	ConfigPath string
	StateDir   string
	HostPort   int
	Env        map[string]string
	Command    []string
}

// DockerArgs builds the argv for `docker run`. It is exported so its shape can
// be asserted without starting a container.
func DockerArgs(cfg StackConfig) []string {
	args := []string{"run", "--rm"}

	if cfg.ConfigPath != "" {
		args = append(args, "-v", cfg.ConfigPath+":/app/config.yaml:ro")
	}
	if cfg.StateDir != "" {
		args = append(args, "-v", cfg.StateDir+":/app/data")
	}
	if cfg.HostPort != 0 {
		args = append(args, "-p", "127.0.0.1:"+strconv.Itoa(cfg.HostPort)+":8080")
	}

	names := make([]string, 0, len(cfg.Env))
	for name := range cfg.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "-e", name+"="+cfg.Env[name])
	}

	args = append(args, imageRepository+":"+cfg.ImageTag)
	return append(args, cfg.Command...)
}

// Stack is a running notifycat container.
type Stack struct {
	containerID string
}

// Start launches the container detached and returns once Docker has accepted
// it. Readiness is proven separately by probing /healthz through the tunnel.
func Start(ctx context.Context, cfg StackConfig) (*Stack, error) {
	args := DockerArgs(cfg)
	// Insert -d directly after "run" so the container detaches.
	detached := append([]string{"run", "-d"}, args[2:]...)

	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, "docker", detached...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("docker run: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	containerID := strings.TrimSpace(stdout.String())
	if containerID == "" {
		return nil, errors.New("docker run returned no container id")
	}
	return &Stack{containerID: containerID}, nil
}

// Logs returns the container's combined output so far. Every run uploads this,
// pass or fail — a passing run's logs are what makes the next failure readable.
func (s *Stack) Logs(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "docker", "logs", s.containerID).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("docker logs: %w", err)
	}
	return string(output), nil
}

// Stop removes the container. It is safe to call on an already-stopped stack.
func (s *Stack) Stop(ctx context.Context) error {
	if s == nil || s.containerID == "" {
		return nil
	}
	if output, err := exec.CommandContext(ctx, "docker", "rm", "-f", s.containerID).CombinedOutput(); err != nil {
		return fmt.Errorf("docker rm: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// RunResult is the outcome of a one-shot container run.
type RunResult struct {
	ExitCode int
	Output   string
}

// RunOnce runs the image to completion and returns its exit code and combined
// output. A non-zero exit is a result, not an error — several scenarios assert
// on exactly that.
func RunOnce(ctx context.Context, cfg StackConfig) (RunResult, error) {
	output, err := exec.CommandContext(ctx, "docker", DockerArgs(cfg)...).CombinedOutput()

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return RunResult{ExitCode: exitError.ExitCode(), Output: string(output)}, nil
	}
	if err != nil {
		return RunResult{Output: string(output)}, fmt.Errorf("docker run: %w", err)
	}
	return RunResult{ExitCode: 0, Output: string(output)}, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/harness/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/harness/stack.go internal/harness/stack_test.go
git commit -m "feat: run the notifycat image as a container or one-shot command"
```

---

### Task 8: `internal/harness` — cloudflared tunnel

**Files:**
- Create: `internal/harness/tunnel.go`
- Test: `internal/harness/tunnel_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `func ParseTunnelURL(output string) (string, bool)`
  - `type Tunnel struct{ URL string; ... }`
  - `func StartTunnel(ctx context.Context, localPort int) (*Tunnel, error)`
  - `func (t *Tunnel) Stop() error`

`StartTunnel` retries three times and probes `GET <url>/healthz` until 200 before returning. A tunnel that never becomes reachable is a hard error — skipping instead would make the whole suite vacuous.

- [ ] **Step 1: Write the failing test**

`internal/harness/tunnel_test.go`:

```go
package harness_test

import (
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/harness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTunnelURLFindsTheQuickTunnelHostname(t *testing.T) {
	testCases := []struct {
		name   string
		output string
		want   string
	}{
		{
			name: "boxed banner",
			output: `2026-09-06T10:00:00Z INF +---------------------------------------+
2026-09-06T10:00:00Z INF |  https://brave-cats-run-fast.trycloudflare.com  |
2026-09-06T10:00:00Z INF +---------------------------------------+`,
			want: "https://brave-cats-run-fast.trycloudflare.com",
		},
		{
			name:   "inline log line",
			output: `INF Your quick Tunnel has been created! Visit it at https://a-b-c.trycloudflare.com`,
			want:   "https://a-b-c.trycloudflare.com",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, found := harness.ParseTunnelURL(testCase.output)

			require.True(t, found)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestParseTunnelURLReportsNoMatch(t *testing.T) {
	_, found := harness.ParseTunnelURL("INF Starting tunnel\nINF Registered")

	assert.False(t, found)
}

func TestParseTunnelURLIgnoresTrailingPunctuation(t *testing.T) {
	got, found := harness.ParseTunnelURL("Visit it at https://a-b-c.trycloudflare.com.")

	require.True(t, found)
	assert.Equal(t, "https://a-b-c.trycloudflare.com", got)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/harness/... -run Tunnel`
Expected: FAIL — `harness.ParseTunnelURL` undefined.

- [ ] **Step 3: Write `internal/harness/tunnel.go`**

```go
package harness

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// quickTunnelURL matches the ephemeral hostname cloudflared prints. The
// hostname is logged in several formats across versions, so the pattern
// deliberately ignores surrounding decoration.
var quickTunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// ParseTunnelURL extracts the quick-tunnel hostname from cloudflared output.
func ParseTunnelURL(output string) (string, bool) {
	match := quickTunnelURL.FindString(output)
	if match == "" {
		return "", false
	}
	return match, true
}

const (
	tunnelStartAttempts = 3
	tunnelReadyTimeout  = 60 * time.Second
)

// Tunnel is a running cloudflared quick tunnel exposing a local port.
type Tunnel struct {
	URL     string
	command *exec.Cmd
	output  *lockedBuffer
}

// StartTunnel opens a quick tunnel to localPort and returns only once
// GET <url>/healthz answers 200 through it. Failure is fatal by design: a
// silent skip would leave the suite asserting nothing.
func StartTunnel(ctx context.Context, localPort int) (*Tunnel, error) {
	var lastErr error
	for attempt := 1; attempt <= tunnelStartAttempts; attempt++ {
		tunnel, err := startTunnelOnce(ctx, localPort)
		if err == nil {
			return tunnel, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("cloudflared did not become reachable after %d attempts: %w",
		tunnelStartAttempts, lastErr)
}

func startTunnelOnce(ctx context.Context, localPort int) (*Tunnel, error) {
	buffer := &lockedBuffer{}
	command := exec.Command("cloudflared", "tunnel", "--no-autoupdate",
		"--url", "http://127.0.0.1:"+strconv.Itoa(localPort))
	command.Stdout = buffer
	command.Stderr = buffer

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}

	tunnel := &Tunnel{command: command, output: buffer}

	url, err := tunnel.awaitURL(ctx)
	if err != nil {
		_ = tunnel.Stop()
		return nil, err
	}
	tunnel.URL = url

	if err := tunnel.awaitHealthy(ctx); err != nil {
		_ = tunnel.Stop()
		return nil, err
	}
	return tunnel, nil
}

func (t *Tunnel) awaitURL(ctx context.Context) (string, error) {
	deadline := time.Now().Add(tunnelReadyTimeout)
	for time.Now().Before(deadline) {
		if url, found := ParseTunnelURL(t.output.String()); found {
			return url, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("no tunnel URL in cloudflared output: %s", strings.TrimSpace(t.output.String()))
}

func (t *Tunnel) awaitHealthy(ctx context.Context) error {
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(tunnelReadyTimeout)

	var lastStatus int
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL+"/healthz", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			lastStatus = response.StatusCode
			_ = response.Body.Close()
			if lastStatus == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("tunnel %s never served /healthz (last status %d)", t.URL, lastStatus)
}

// Stop terminates cloudflared. Safe to call more than once.
func (t *Tunnel) Stop() error {
	if t == nil || t.command == nil || t.command.Process == nil {
		return nil
	}
	if err := t.command.Process.Kill(); err != nil {
		return fmt.Errorf("kill cloudflared: %w", err)
	}
	_ = t.command.Wait()
	return nil
}

// lockedBuffer collects cloudflared's output while the reader polls it.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/harness/...`
Expected: PASS

- [ ] **Step 5: Verify a tunnel actually opens locally**

```bash
python3 -m http.server 8099 >/dev/null 2>&1 &
cloudflared tunnel --no-autoupdate --url http://127.0.0.1:8099 2>&1 | head -20
```

Expected: a `https://*.trycloudflare.com` URL appears. Kill both afterwards. If `cloudflared` is absent, install it (`brew install cloudflared`) before continuing.

- [ ] **Step 6: Commit**

```bash
git add internal/harness/tunnel.go internal/harness/tunnel_test.go
git commit -m "feat: open and health-check a cloudflared quick tunnel"
```

---

### Task 9: Validation, doctor and config-CLI scenarios

**Files:**
- Create: `tests/validation_test.go`

**Interfaces:**
- Consumes: `testenv.Load`, `testenv.SkipUnless`, `harness.WriteConfig`, `harness.RunOnce`, `harness.StackConfig`, `harness.RunResult`.
- Produces: nothing later tasks consume.

These run first because they need no tunnel and no repository action — a broken image fails here in seconds, before anything touches GitHub.

They run the image with `notifycat-config` and `notifycat-doctor` and assert exit codes, stdout, and the `config.lock` written next to the mounted config. The lock lands beside `config.yaml`, so the config is mounted from a writable temp directory rather than read-only for these scenarios.

- [ ] **Step 1: Write the scenarios**

`tests/validation_test.go`:

```go
//go:build integration

package tests_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mptooling/notifycat-integration-tests/internal/harness"
	"github.com/mptooling/notifycat-integration-tests/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const configTemplate = "../config/config.yaml.tmpl"

// validationStack prepares a writable directory holding a rendered config so
// notifycat can write config.lock beside it, and returns the stack config for
// a one-shot CLI run.
func validationStack(t *testing.T, env testenv.Env, command []string, badChannel bool) (harness.StackConfig, string) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o777))

	configPath, err := harness.WriteConfig(dir, configTemplate, harness.ConfigInput{
		Channels:   env.Channels,
		Fixtures:   env.Fixtures,
		BadChannel: badChannel,
	})
	require.NoError(t, err)
	require.NoError(t, os.Chmod(configPath, 0o666))

	return harness.StackConfig{
		ImageTag: env.ImageTag,
		StateDir: dir,
		Env: map[string]string{
			"SLACK_BOT_TOKEN":       env.SlackBotToken,
			"GITHUB_WEBHOOK_SECRET": env.WebhookSecret,
			"GITHUB_TOKEN":          env.GitHubAuthorToken,
			"NOTIFYCAT_CONFIG_FILE": "/app/data/config.yaml",
		},
		Command: command,
	}, dir
}

func readLock(t *testing.T, dir string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dir, "config.lock"))
	require.NoError(t, err, "validation must write a lock next to the config")

	var lock struct {
		Entries map[string]any `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(raw, &lock))
	return lock.Entries
}

func TestValidationConfigListShowsEveryFixture(t *testing.T) {
	env, err := testenv.Load()
	require.NoError(t, err)

	stack, _ := validationStack(t, env, []string{"/usr/local/bin/notifycat-config", "list"}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := harness.RunOnce(ctx, stack)

	require.NoError(t, err)
	assert.Zero(t, result.ExitCode, result.Output)
	assert.Contains(t, result.Output, "notifycat-it-alpha")
	assert.Contains(t, result.Output, "notifycat-it-mono")
	assert.Contains(t, result.Output, "notifycat-it-nohook")
}

func TestValidationWarnsOnTheRepositoryWithNoWebhook(t *testing.T) {
	env, err := testenv.Load()
	require.NoError(t, err)
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": env.Channels.Primary})

	stack, dir := validationStack(t, env,
		[]string{"/usr/local/bin/notifycat-config", "validate"}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := harness.RunOnce(ctx, stack)

	require.NoError(t, err)
	assert.Zero(t, result.ExitCode, "a warning is advisory and never fails validation:\n%s", result.Output)
	assert.Contains(t, result.Output, "notifycat-it-nohook")
	assert.Contains(t, result.Output, "WARN")

	entries := readLock(t, dir)
	assert.NotContains(t, entries, "mptooling/notifycat-it-nohook",
		"a warned entry is never cached")
	assert.Contains(t, entries, "mptooling/notifycat-it-alpha")
}

func TestValidationReProbesTheWarnedEntryOnEveryRun(t *testing.T) {
	env, err := testenv.Load()
	require.NoError(t, err)
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": env.Channels.Primary})

	stack, dir := validationStack(t, env,
		[]string{"/usr/local/bin/notifycat-config", "validate"}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	first, err := harness.RunOnce(ctx, stack)
	require.NoError(t, err)
	require.Zero(t, first.ExitCode, first.Output)

	second, err := harness.RunOnce(ctx, stack)

	require.NoError(t, err)
	assert.Zero(t, second.ExitCode, second.Output)
	assert.Contains(t, second.Output, "WARN",
		"the warned entry stayed out of the lock, so it must be re-probed")
	assert.NotContains(t, readLock(t, dir), "mptooling/notifycat-it-nohook")
}

func TestValidationDoctorReportsPerRepositorySections(t *testing.T) {
	env, err := testenv.Load()
	require.NoError(t, err)
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": env.Channels.Primary})

	stack, _ := validationStack(t, env,
		[]string{"/usr/local/bin/notifycat-doctor", env.Fixtures.Alpha}, false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := harness.RunOnce(ctx, stack)

	require.NoError(t, err)
	assert.Zero(t, result.ExitCode, result.Output)
	assert.Contains(t, result.Output, "[config]")
	assert.Contains(t, result.Output, "OK")
}

func TestValidationStartupAbortsOnAFailingChannel(t *testing.T) {
	env, err := testenv.Load()
	require.NoError(t, err)
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": env.Channels.Primary})

	stack, _ := validationStack(t, env, nil, true)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := harness.RunOnce(ctx, stack)

	require.NoError(t, err)
	assert.NotZero(t, result.ExitCode, "a FAIL check must abort startup:\n%s", result.Output)
	assert.Contains(t, result.Output, "C0000000BAD")
}
```

- [ ] **Step 2: Run the scenarios against the current image**

```bash
export NOTIFYCAT_IMAGE_TAG=latest
just test-validation
```

Expected: PASS, or a clear skip naming a missing secret. If the image tag `edge` does not exist yet, `latest` is the right stand-in until Task 13 lands.

- [ ] **Step 3: Commit**

```bash
git add tests/validation_test.go
git commit -m "test: assert the validation, doctor and startup-gate behaviour"
```

---

### Task 10: Suite bootstrap and the pull-request lifecycle scenarios

**Files:**
- Create: `tests/main_test.go`
- Create: `tests/lifecycle_test.go`

**Interfaces:**
- Consumes: everything from Tasks 3–8.
- Produces, for Task 11:
  - `var suite *suiteContext`
  - `type suiteContext struct{ Env testenv.Env; GitHub *githubx.Client; Slack *slackx.Client; TunnelURL string; Stack *harness.Stack }`
  - `func openFixturePR(t *testing.T, repoFullName, branchSuffix string, files map[string]string) githubx.PullRequest`
  - `func awaitMessage(t *testing.T, channelID string) slackx.Message`
  - `func awaitReaction(t *testing.T, channelID, timestamp, emoji string)`
  - `func assertNoMessage(t *testing.T, channelID string)`

`TestMain` owns the shared stack and tunnel; validation scenarios ignore both because they build their own one-shot runs.

- [ ] **Step 1: Write `tests/main_test.go`**

```go
//go:build integration

package tests_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat-integration-tests/internal/githubx"
	"github.com/mptooling/notifycat-integration-tests/internal/harness"
	"github.com/mptooling/notifycat-integration-tests/internal/slackx"
	"github.com/mptooling/notifycat-integration-tests/internal/testenv"
	"github.com/stretchr/testify/require"
)

const (
	hostPort          = 18080
	slackPollTimeout  = 60 * time.Second
	slackPollInterval = 2 * time.Second
)

type suiteContext struct {
	Env       testenv.Env
	GitHub    *githubx.Client
	Slack     *slackx.Client
	TunnelURL string
	Stack     *harness.Stack

	createdWebhooks  []createdWebhook
	postedTimestamps []postedMessage
}

type createdWebhook struct {
	repoFullName string
	id           int64
}

type postedMessage struct {
	channelID string
	timestamp string
}

var suite *suiteContext

// TestMain brings up the container and the tunnel once for the whole run.
// Validation scenarios do not use either — they build their own one-shot runs.
func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "suite setup failed:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	env, err := testenv.Load()
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	stateDir, err := os.MkdirTemp("", "notifycat-it-")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(stateDir) }()
	if err := os.Chmod(stateDir, 0o777); err != nil {
		return 0, err
	}

	configPath, err := harness.WriteConfig(stateDir, "../config/config.yaml.tmpl",
		harness.ConfigInput{Channels: env.Channels, Fixtures: env.Fixtures})
	if err != nil {
		return 0, err
	}
	if err := os.Chmod(configPath, 0o666); err != nil {
		return 0, err
	}

	stack, err := harness.Start(ctx, harness.StackConfig{
		ImageTag: env.ImageTag,
		StateDir: stateDir,
		HostPort: hostPort,
		Env: map[string]string{
			"SLACK_BOT_TOKEN":       env.SlackBotToken,
			"GITHUB_WEBHOOK_SECRET": env.WebhookSecret,
			"GITHUB_TOKEN":          env.GitHubAuthorToken,
			"NOTIFYCAT_CONFIG_FILE": "/app/data/config.yaml",
		},
	})
	if err != nil {
		return 0, err
	}
	defer func() {
		if logs, logErr := stack.Logs(ctx); logErr == nil {
			_ = os.WriteFile("notifycat-container.log", []byte(logs), 0o644)
		}
		_ = stack.Stop(ctx)
	}()

	tunnel, err := harness.StartTunnel(ctx, hostPort)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tunnel.Stop() }()

	suite = &suiteContext{
		Env:       env,
		TunnelURL: tunnel.URL,
		Stack:     stack,
		GitHub:    githubx.NewClient(githubx.ClientConfig{BaseURL: "https://api.github.com", Token: env.GitHubAuthorToken}),
		Slack:     slackx.NewClient(slackx.ClientConfig{BaseURL: "https://slack.com", Token: env.SlackBotToken}),
	}

	suite.sweepOrphanWebhooks(ctx)

	code := m.Run()
	suite.cleanup(ctx)
	return code, nil
}

// sweepOrphanWebhooks deletes quick-tunnel hooks left behind by a run that
// crashed before its own teardown. Their tunnels are already dead, so GitHub
// would otherwise retry deliveries into nothing for days.
func (s *suiteContext) sweepOrphanWebhooks(ctx context.Context) {
	fixtures := []string{s.Env.Fixtures.Alpha, s.Env.Fixtures.Mono, s.Env.Fixtures.NoHook}

	for _, repoFullName := range fixtures {
		hooks, err := s.GitHub.ListWebhooks(ctx, repoFullName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sweep %s: %v\n", repoFullName, err)
			continue
		}
		for _, hook := range hooks {
			if !strings.Contains(hook.URL, ".trycloudflare.com") || strings.HasPrefix(hook.URL, s.TunnelURL) {
				continue
			}
			if err := s.GitHub.DeleteWebhook(ctx, repoFullName, hook.ID); err != nil {
				fmt.Fprintf(os.Stderr, "sweep %s hook %d: %v\n", repoFullName, hook.ID, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "swept orphan webhook %d on %s\n", hook.ID, repoFullName)
		}
	}
}

// cleanup removes everything the run created, in reverse order of creation. It
// runs after failures too, so every step tolerates an already-absent target.
func (s *suiteContext) cleanup(ctx context.Context) {
	for _, message := range s.postedTimestamps {
		_ = s.Slack.Delete(ctx, message.channelID, message.timestamp)
	}
	for _, hook := range s.createdWebhooks {
		_ = s.GitHub.DeleteWebhook(ctx, hook.repoFullName, hook.id)
	}
}

// webhookFor points a fixture repository at this run's tunnel and registers the
// hook for teardown. Each scenario creates its own, so a hook orphaned by a
// crashed run can never block the next one.
func webhookFor(t *testing.T, repoFullName string) {
	t.Helper()

	hook, err := suite.GitHub.CreateWebhook(context.Background(), githubx.CreateWebhookRequest{
		Repo:   repoFullName,
		URL:    suite.TunnelURL + "/webhook/github",
		Secret: suite.Env.WebhookSecret,
	})
	require.NoError(t, err)

	suite.createdWebhooks = append(suite.createdWebhooks, createdWebhook{repoFullName: repoFullName, id: hook.ID})
	t.Cleanup(func() {
		_ = suite.GitHub.DeleteWebhook(context.Background(), repoFullName, hook.ID)
	})
}

// openFixturePR pushes a branch carrying files and opens a pull request whose
// title embeds the run marker.
func openFixturePR(t *testing.T, repoFullName, branchSuffix string, files map[string]string) githubx.PullRequest {
	t.Helper()

	ctx := context.Background()
	branch := fmt.Sprintf("it-%s-%s", suite.Env.RunID, branchSuffix)

	require.NoError(t, suite.GitHub.CommitFilesOnNewBranch(ctx, githubx.CommitFilesRequest{
		Repo:    repoFullName,
		Branch:  branch,
		Message: "test: integration run " + suite.Env.RunID,
		Files:   files,
	}))

	pullRequest, err := suite.GitHub.OpenPullRequest(ctx, githubx.OpenPullRequestRequest{
		Repo:  repoFullName,
		Head:  branch,
		Base:  "main",
		Title: fmt.Sprintf("integration %s %s", branchSuffix, suite.Env.Marker()),
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = suite.GitHub.ClosePullRequest(ctx, repoFullName, pullRequest.Number)
		_ = suite.GitHub.DeleteBranch(ctx, repoFullName, branch)
	})
	return pullRequest
}

// awaitMessage polls a channel until this run's message appears.
func awaitMessage(t *testing.T, channelID string) slackx.Message {
	t.Helper()

	var found slackx.Message
	require.Eventually(t, func() bool {
		message, ok, err := suite.Slack.FindByMarker(context.Background(), channelID, suite.Env.Marker())
		if err != nil || !ok {
			return false
		}
		found = message
		return true
	}, slackPollTimeout, slackPollInterval, "no message carrying %s reached %s", suite.Env.Marker(), channelID)

	suite.postedTimestamps = append(suite.postedTimestamps,
		postedMessage{channelID: channelID, timestamp: found.Timestamp})
	return found
}

// awaitReaction polls reactions.get until the emoji lands on the message.
func awaitReaction(t *testing.T, channelID, timestamp, emoji string) {
	t.Helper()

	require.Eventually(t, func() bool {
		names, err := suite.Slack.Reactions(context.Background(), channelID, timestamp)
		if err != nil {
			return false
		}
		for _, name := range names {
			if name == emoji {
				return true
			}
		}
		return false
	}, slackPollTimeout, slackPollInterval, "reaction %s never landed on %s/%s", emoji, channelID, timestamp)
}

// assertNoMessage gives delivery a fair chance and then asserts the channel
// stayed empty for this run.
func assertNoMessage(t *testing.T, channelID string) {
	t.Helper()

	time.Sleep(15 * time.Second)

	_, found, err := suite.Slack.FindByMarker(context.Background(), channelID, suite.Env.Marker())
	require.NoError(t, err)
	require.False(t, found, "channel %s received a message it should not have", channelID)
}
```

- [ ] **Step 2: Write `tests/lifecycle_test.go`**

```go
//go:build integration

package tests_test

import (
	"context"
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/githubx"
	"github.com/mptooling/notifycat-integration-tests/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifecycleOpenPostsAndReacts(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": suite.Env.Channels.Primary})
	webhookFor(t, suite.Env.Fixtures.Alpha)

	pullRequest := openFixturePR(t, suite.Env.Fixtures.Alpha, "open",
		map[string]string{"src/app.txt": "opened by " + suite.Env.RunID})

	message := awaitMessage(t, suite.Env.Channels.Primary)

	assert.Contains(t, message.Text, suite.Env.Marker())
	assert.Contains(t, message.Text, pullRequest.HTMLURL)
	awaitReaction(t, suite.Env.Channels.Primary, message.Timestamp, "eyes")
}

func TestLifecycleCommentReviewAddsTheCommentedReaction(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": suite.Env.Channels.Primary})
	webhookFor(t, suite.Env.Fixtures.Alpha)

	pullRequest := openFixturePR(t, suite.Env.Fixtures.Alpha, "comment",
		map[string]string{"src/app.txt": "commented by " + suite.Env.RunID})
	message := awaitMessage(t, suite.Env.Channels.Primary)

	require.NoError(t, suite.GitHub.SubmitReview(context.Background(), githubx.ReviewRequest{
		Repo:   suite.Env.Fixtures.Alpha,
		Number: pullRequest.Number,
		Event:  "COMMENT",
		Body:   "integration comment review",
	}))

	awaitReaction(t, suite.Env.Channels.Primary, message.Timestamp, "speech_balloon")
}

func TestLifecycleMergeUpdatesTheMessageInPlace(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": suite.Env.Channels.Primary})
	webhookFor(t, suite.Env.Fixtures.Alpha)

	pullRequest := openFixturePR(t, suite.Env.Fixtures.Alpha, "merge",
		map[string]string{"src/app.txt": "merged by " + suite.Env.RunID})
	opened := awaitMessage(t, suite.Env.Channels.Primary)

	require.NoError(t, suite.GitHub.MergePullRequest(context.Background(),
		suite.Env.Fixtures.Alpha, pullRequest.Number))

	awaitReaction(t, suite.Env.Channels.Primary, opened.Timestamp, "twisted_rightwards_arrows")

	updated := awaitMessage(t, suite.Env.Channels.Primary)
	assert.Equal(t, opened.Timestamp, updated.Timestamp,
		"a merge updates the existing message rather than posting a new one")
}

func TestLifecycleCloseWithoutMergeAddsTheClosedReaction(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{"SLACK_CHANNEL_PRIMARY": suite.Env.Channels.Primary})
	webhookFor(t, suite.Env.Fixtures.Alpha)

	pullRequest := openFixturePR(t, suite.Env.Fixtures.Alpha, "close",
		map[string]string{"src/app.txt": "closed by " + suite.Env.RunID})
	message := awaitMessage(t, suite.Env.Channels.Primary)

	require.NoError(t, suite.GitHub.ClosePullRequest(context.Background(),
		suite.Env.Fixtures.Alpha, pullRequest.Number))

	awaitReaction(t, suite.Env.Channels.Primary, message.Timestamp, "x")
}

func TestLifecycleApprovalNeedsTheReviewerIdentity(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{
		"REVIEWER_APP_ID":          suite.Env.ReviewerAppID,
		"REVIEWER_APP_PRIVATE_KEY": suite.Env.ReviewerPrivateKey,
	})

	t.Skip("reviewer identity wiring lands with the GitHub App; see the plan's Task 10 note")
}
```

The approval scenario is deliberately a declared skip rather than a missing test: the GitHub App does not exist yet, and a named skip is the honest record of that gap. When the App is installed, replace the `t.Skip` with a review submitted through an installation token and an `awaitReaction` on `white_check_mark`.

- [ ] **Step 3: Run the lifecycle scenarios**

```bash
just test
```

Expected: the lifecycle tests PASS, or skip with a named missing secret. Inspect `tests/notifycat-container.log` if a Slack assertion times out — the container log shows whether the delivery arrived at all.

- [ ] **Step 4: Add the log file to `.gitignore` and commit**

```bash
echo "/tests/notifycat-container.log" >> .gitignore
git add tests/main_test.go tests/lifecycle_test.go .gitignore
git commit -m "test: assert the pull-request lifecycle end to end"
```

---

### Task 11: Routing scenarios

**Files:**
- Create: `tests/routing_test.go`

**Interfaces:**
- Consumes: the helpers Task 10 produced — `webhookFor`, `openFixturePR`, `awaitMessage`, `assertNoMessage`, and `suite`.
- Produces: nothing later tasks consume.

- [ ] **Step 1: Write the scenarios**

`tests/routing_test.go`:

```go
//go:build integration

package tests_test

import (
	"testing"

	"github.com/mptooling/notifycat-integration-tests/internal/testenv"
	"github.com/stretchr/testify/assert"
)

func TestRoutingFansOutToEveryDeclaredChannel(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{
		"SLACK_CHANNEL_PRIMARY":   suite.Env.Channels.Primary,
		"SLACK_CHANNEL_SECONDARY": suite.Env.Channels.Secondary,
	})
	webhookFor(t, suite.Env.Fixtures.Alpha)

	openFixturePR(t, suite.Env.Fixtures.Alpha, "fanout",
		map[string]string{"src/app.txt": "fanout by " + suite.Env.RunID})

	primary := awaitMessage(t, suite.Env.Channels.Primary)
	secondary := awaitMessage(t, suite.Env.Channels.Secondary)

	assert.Contains(t, primary.Text, "<!channel>",
		"the first entry omits mentions, so it inherits the @channel fallback")
	assert.NotContains(t, secondary.Text, "<!channel>",
		"the second entry sets mentions: [], so it posts silently")
}

func TestRoutingSendsAPathMatchOnlyToItsTeamChannel(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{
		"SLACK_CHANNEL_TEAM_A": suite.Env.Channels.TeamA,
		"SLACK_CHANNEL_TEAM_B": suite.Env.Channels.TeamB,
	})
	webhookFor(t, suite.Env.Fixtures.Mono)

	openFixturePR(t, suite.Env.Fixtures.Mono, "path-a",
		map[string]string{"modules/acme/service.txt": "touched by " + suite.Env.RunID})

	message := awaitMessage(t, suite.Env.Channels.TeamA)

	assert.Contains(t, message.Text, suite.Env.Marker())
	assertNoMessage(t, suite.Env.Channels.TeamB)
}

func TestRoutingFansOutAcrossTwoMatchedPaths(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{
		"SLACK_CHANNEL_TEAM_A": suite.Env.Channels.TeamA,
		"SLACK_CHANNEL_TEAM_B": suite.Env.Channels.TeamB,
	})
	webhookFor(t, suite.Env.Fixtures.Mono)

	openFixturePR(t, suite.Env.Fixtures.Mono, "path-ab", map[string]string{
		"modules/acme/service.txt":  "touched by " + suite.Env.RunID,
		"modules/betta/service.txt": "touched by " + suite.Env.RunID,
	})

	teamA := awaitMessage(t, suite.Env.Channels.TeamA)
	teamB := awaitMessage(t, suite.Env.Channels.TeamB)

	assert.Contains(t, teamA.Text, suite.Env.Marker())
	assert.Contains(t, teamB.Text, suite.Env.Marker())
}

func TestRoutingFallsBackToTheBaseChannelWhenNoPathMatches(t *testing.T) {
	testenv.SkipUnless(t, map[string]string{
		"SLACK_CHANNEL_PRIMARY": suite.Env.Channels.Primary,
		"SLACK_CHANNEL_TEAM_A":  suite.Env.Channels.TeamA,
	})
	webhookFor(t, suite.Env.Fixtures.Mono)

	openFixturePR(t, suite.Env.Fixtures.Mono, "path-none",
		map[string]string{"README.md": "untouched area, run " + suite.Env.RunID})

	message := awaitMessage(t, suite.Env.Channels.Primary)

	assert.Contains(t, message.Text, suite.Env.Marker())
	assertNoMessage(t, suite.Env.Channels.TeamA)
}
```

Path routing reads a pull request's changed files from the GitHub API. The container is given `GITHUB_TOKEN` in Task 10's `TestMain`; without it every path rule is inert and these scenarios would silently collapse into the fallback case.

- [ ] **Step 2: Run them**

```bash
just test
```

Expected: PASS or named skips.

- [ ] **Step 3: Commit**

```bash
git add tests/routing_test.go
git commit -m "test: assert channel fan-out and monorepo path routing"
```

---

### Task 12: CI workflow and failure reporting

**Files:**
- Create: `.github/workflows/integration.yml`

**Interfaces:**
- Consumes: everything above.
- Produces: nothing consumed by other tasks.

- [ ] **Step 1: Write `.github/workflows/integration.yml`**

```yaml
name: integration

# Runs the suite against a published notifycat image. Never a required check:
# a flaking quick tunnel must not be able to block a release.
on:
  repository_dispatch:
    types: [notifycat-image]
  push:
    branches:
      - main
  pull_request:
  schedule:
    # Nightly at 03:00 UTC — off-peak, and well clear of the digest hour.
    - cron: "0 3 * * *"
  workflow_dispatch:
    inputs:
      tag:
        description: "notifycat image tag to test"
        required: false
        default: "edge"
        type: string

permissions:
  contents: read
  issues: write

# One suite at a time: the fixture repositories and Slack channels are shared.
concurrency:
  group: integration
  cancel-in-progress: false

env:
  GO_VERSION: "1.25.14"

jobs:
  integration:
    name: Run the integration suite
    runs-on: ubuntu-latest
    timeout-minutes: 25

    steps:
      - name: Check out repository
        uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v7
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true

      - name: Resolve the image tag
        id: image
        env:
          DISPATCH_TAG: ${{ github.event.client_payload.tag }}
          INPUT_TAG: ${{ inputs.tag }}
        run: |
          set -eu
          tag="${DISPATCH_TAG:-${INPUT_TAG:-edge}}"
          case "$tag" in
            *[!A-Za-z0-9._-]*|'') echo "::error::invalid image tag '${tag}'"; exit 1 ;;
          esac
          echo "tag=${tag}" >> "$GITHUB_OUTPUT"

      - name: Install cloudflared
        run: |
          set -eu
          curl -fsSL -o cloudflared \
            https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64
          chmod +x cloudflared
          sudo mv cloudflared /usr/local/bin/cloudflared
          cloudflared --version

      - name: Pull the image under test
        run: docker pull ghcr.io/mptooling/notifycat:${{ steps.image.outputs.tag }}

      - name: Run the suite
        env:
          NOTIFYCAT_IMAGE_TAG: ${{ steps.image.outputs.tag }}
          NOTIFYCAT_WEBHOOK_SECRET: ${{ secrets.NOTIFYCAT_WEBHOOK_SECRET }}
          SLACK_BOT_TOKEN: ${{ secrets.SLACK_BOT_TOKEN }}
          SLACK_CHANNEL_PRIMARY: ${{ secrets.SLACK_CHANNEL_PRIMARY }}
          SLACK_CHANNEL_SECONDARY: ${{ secrets.SLACK_CHANNEL_SECONDARY }}
          SLACK_CHANNEL_TEAM_A: ${{ secrets.SLACK_CHANNEL_TEAM_A }}
          SLACK_CHANNEL_TEAM_B: ${{ secrets.SLACK_CHANNEL_TEAM_B }}
          GITHUB_AUTHOR_TOKEN: ${{ secrets.GITHUB_AUTHOR_TOKEN }}
          REVIEWER_APP_ID: ${{ secrets.REVIEWER_APP_ID }}
          REVIEWER_APP_PRIVATE_KEY: ${{ secrets.REVIEWER_APP_PRIVATE_KEY }}
        run: go test -tags=integration -timeout=20m -v ./tests/...

      - name: Upload the container log
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: notifycat-container-log-${{ github.run_id }}
          path: tests/notifycat-container.log
          if-no-files-found: warn

      - name: Report the failure
        if: failure() && github.event_name != 'pull_request'
        env:
          GH_TOKEN: ${{ github.token }}
          SLACK_BOT_TOKEN: ${{ secrets.SLACK_BOT_TOKEN }}
          SLACK_CHANNEL_ALERTS: ${{ secrets.SLACK_CHANNEL_ALERTS }}
          TAG: ${{ steps.image.outputs.tag }}
          RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
        run: |
          set -eu
          marker="<!-- integration-failure -->"
          title="Integration suite failing"
          body="$(printf '%s\nThe integration suite failed against `%s`.\n\nLatest run: %s\n' \
            "$marker" "$TAG" "$RUN_URL")"

          existing="$(gh issue list --state open --json number,body \
            --jq "map(select(.body | startswith(\"${marker}\"))) | .[0].number // empty")"
          if [ -n "$existing" ]; then
            gh issue comment "$existing" --body "Still failing against \`${TAG}\`: ${RUN_URL}"
          else
            gh issue create --title "$title" --body "$body" --label bug
          fi

          if [ -n "${SLACK_CHANNEL_ALERTS:-}" ]; then
            curl -fsS -X POST https://slack.com/api/chat.postMessage \
              -H "Authorization: Bearer ${SLACK_BOT_TOKEN}" \
              -H 'Content-type: application/json; charset=utf-8' \
              -d "$(printf '{"channel":"%s","text":"Notifycat integration suite failed against `%s` — %s"}' \
                "$SLACK_CHANNEL_ALERTS" "$TAG" "$RUN_URL")" >/dev/null
          fi
```

The failure step is skipped on pull requests: a contributor's failing branch should not open an issue or page the channel.

- [ ] **Step 2: Verify the workflow parses**

```bash
gh workflow list 2>/dev/null || true
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/integration.yml')); print('valid yaml')"
```

Expected: `valid yaml`.

- [ ] **Step 3: Commit and push**

```bash
git add .github/workflows/integration.yml
git commit -m "ci: run the integration suite on dispatch, push and nightly"
git push
```

- [ ] **Step 4: Trigger a manual run once the secrets are in place**

```bash
gh workflow run integration --repo mptooling/notifycat-integration-tests -f tag=latest
gh run watch --repo mptooling/notifycat-integration-tests
```

Expected: green, with skips naming any secret still missing.

---

### Task 13: Publish `:edge` and dispatch from notifycat

**Files:**
- Create: `/Users/pavlomaksymov/projects/notifycat/.github/workflows/docker-edge.yml`

**Interfaces:**
- Consumes: nothing.
- Produces: the `ghcr.io/mptooling/notifycat:edge` tag and the `notifycat-image` repository dispatch the Task 12 workflow listens for.

This is a **separate pull request in the notifycat repository**, not in the integration repository. The existing `pr-<n>` beta tag cannot serve here: `docker-pr.yml` deletes it when the pull request closes, which is the same moment the merge to `main` happens.

- [ ] **Step 1: Create the branch**

```bash
cd /Users/pavlomaksymov/projects/notifycat
git checkout main
git pull
git checkout -b ci/edge-image-and-integration-dispatch
```

- [ ] **Step 2: Write `.github/workflows/docker-edge.yml`**

```yaml
name: docker-edge

# Publishes a moving :edge tag for every commit on main and asks the
# integration suite to run against it. The pr-<number> tag cannot serve this
# purpose: docker-pr.yml deletes it the moment the pull request closes, which
# is the same moment the merge lands on main.
on:
  push:
    branches:
      - main
  release:
    types: [published]
  workflow_dispatch:

permissions:
  contents: read
  packages: write

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

concurrency:
  group: docker-edge
  cancel-in-progress: true

jobs:
  publish:
    name: Publish and dispatch
    runs-on: ubuntu-latest

    steps:
      - name: Check out repository
        uses: actions/checkout@v7

      - name: Set up QEMU
        uses: docker/setup-qemu-action@v4
        with:
          platforms: arm64

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4

      - name: Log in to GHCR
        uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Resolve the tag to test
        id: target
        env:
          RELEASE_TAG: ${{ github.event.release.tag_name }}
        run: |
          set -eu
          if [ -n "${RELEASE_TAG:-}" ]; then
            echo "tag=${RELEASE_TAG}" >> "$GITHUB_OUTPUT"
            echo "push=false" >> "$GITHUB_OUTPUT"
          else
            echo "tag=edge" >> "$GITHUB_OUTPUT"
            echo "push=true" >> "$GITHUB_OUTPUT"
          fi

      - name: Build and push the edge image
        if: steps.target.outputs.push == 'true'
        uses: docker/build-push-action@v7
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: true
          tags: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:edge
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Ask the integration suite to run
        env:
          GH_TOKEN: ${{ secrets.INTEGRATION_DISPATCH_TOKEN }}
          TAG: ${{ steps.target.outputs.tag }}
        run: |
          set -eu
          if [ -z "${GH_TOKEN:-}" ]; then
            echo "::warning::INTEGRATION_DISPATCH_TOKEN is unset — skipping the dispatch"
            exit 0
          fi
          gh api -X POST repos/mptooling/notifycat-integration-tests/dispatches \
            -f event_type=notifycat-image \
            -F "client_payload[tag]=${TAG}"
          echo "dispatched integration run for ${TAG}"
```

The release branch dispatches without rebuilding: `docker-release.yml` already published that tag.

- [ ] **Step 3: Verify the workflow parses**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/docker-edge.yml')); print('valid yaml')"
```

Expected: `valid yaml`.

- [ ] **Step 4: Commit, push and open the pull request with an empty body**

```bash
git add .github/workflows/docker-edge.yml
git commit -m "ci: publish an edge image and trigger the integration suite"
git push -u origin ci/edge-image-and-integration-dispatch
gh pr create --title "ci: publish an edge image and trigger the integration suite" --body ""
```

- [ ] **Step 5: After merge, confirm the tag and the dispatch**

```bash
gh api "orgs/mptooling/packages/container/notifycat/versions" \
  --jq '.[] | select(.metadata.container.tags[]? == "edge") | .id'
gh run list --repo mptooling/notifycat-integration-tests --limit 3
```

Expected: an `edge` package version exists and a dispatched `integration` run appears.

---

## Post-implementation checklist for the operator

These need a browser and cannot be done by an agent. The suite is green without them, but it skips the scenarios each one unlocks.

- [ ] Slack app with `chat:write`, `channels:read`, `channels:history`, `reactions:read`, `reactions:write`; install; copy the bot token.
- [ ] Five channels created, bot invited to each, IDs recorded.
- [ ] Fine-grained PAT (author) on the three fixture repositories: contents write, pull requests write, webhooks admin.
- [ ] GitHub App (reviewer) installed on the three fixture repositories; unlocks `TestLifecycleApprovalNeedsTheReviewerIdentity`.
- [ ] Fine-grained PAT with Actions: write on `notifycat-integration-tests`, stored in **notifycat** as `INTEGRATION_DISPATCH_TOKEN`.
- [ ] All suite secrets added to `notifycat-integration-tests`.
- [ ] Confirm `ghcr.io/mptooling/notifycat` is pullable from the private integration repository's runner. If the package is private, make it public or add a read token to the workflow.
