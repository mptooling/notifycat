package runtime_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/security"
	"github.com/mptooling/notifycat/internal/runtime"
)

func buildTestServer(t *testing.T, cfg config.Config) *http.Server {
	t.Helper()

	var server *http.Server
	var db *gorm.DB
	app := fx.New(
		fx.Supply(cfg),
		runtime.Module,
		fx.NopLogger,
		fx.Populate(&server, &db),
	)
	require.NoError(t, app.Err(), "build runtime graph")
	t.Cleanup(func() {
		if sqlDB, dbErr := persistence.SQLDB(db); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return server
}

func newTestConfig(t *testing.T) config.Config {
	t.Helper()

	dir := t.TempDir()
	return config.Config{
		Addr:        ":0",
		LogLevel:    "error",
		LogFormat:   "text",
		DatabaseURL: "file:" + filepath.Join(dir, "wire.db"),
		ConfigFile:  filepath.Join(dir, "config.yaml"),

		MessageTTLDays: 30,

		GitHubWebhookSecret: config.Secret("topsecret"),
		SlackBotToken:       config.Secret("xoxb-test"),
		Reactions: config.Reactions{
			Enabled:       true,
			NewPR:         "rocket",
			MergedPR:      "twisted_rightwards_arrows",
			ClosedPR:      "x",
			Approved:      "white_check_mark",
			Commented:     "speech_balloon",
			RequestChange: "exclamation",
		},
	}
}

// serve exposes the wired handler over httptest for the duration of the test.
func serve(t *testing.T, cfg config.Config) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(buildTestServer(t, cfg).Handler)
	t.Cleanup(server.Close)
	return server
}

// request drives one HTTP call against the wired server and returns its status
// and body.
func request(t *testing.T, server *httptest.Server, method, path, body string, headers map[string]string) (int, string) {
	t.Helper()

	request, err := http.NewRequestWithContext(context.Background(), method, server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()

	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(responseBody)
}

// githubSignature signs a webhook body the way GitHub does.
func githubSignature(secret string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// slackSignature signs an interaction body the way Slack does.
func slackSignature(secret, timestamp, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write([]byte(body))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWire_ReturnsServerAndCleanup(t *testing.T) {
	server := buildTestServer(t, newTestConfig(t))

	require.NotNil(t, server)
	assert.NotNil(t, server.Handler)
}

func TestWire_HealthzReturns200(t *testing.T) {
	server := serve(t, newTestConfig(t))

	status, _ := request(t, server, http.MethodGet, "/healthz", "", nil)

	assert.Equal(t, http.StatusOK, status)
}

func TestWire_RejectsUnsignedWebhook(t *testing.T) {
	server := serve(t, newTestConfig(t))

	status, _ := request(t, server, http.MethodPost, "/webhook/github", `{"action":"opened"}`,
		map[string]string{"Content-Type": "application/json"})

	assert.Equal(t, http.StatusUnauthorized, status)
}

func TestWire_AcceptsSignedWebhookButHasNoMapping(t *testing.T) {
	server := serve(t, newTestConfig(t))
	body := `{
		"action": "opened",
		"repository": {"full_name": "octo/no-mapping"},
		"pull_request": {"number": 1, "title": "t", "html_url": "u", "user": {"login": "a"}}
	}`

	status, responseBody := request(t, server, http.MethodPost, "/webhook/github", body, map[string]string{
		"X-Hub-Signature-256": githubSignature("topsecret", body),
		"Content-Type":        "application/json",
	})

	assert.Equal(t, http.StatusOK, status, "an unmapped repo is a silent no-op: %s", responseBody)
}

func TestWire_SlackInteractionsAbsentWithoutSigningSecret(t *testing.T) {
	server := serve(t, newTestConfig(t))

	status, _ := request(t, server, http.MethodPost, "/webhook/slack/interactions", "payload=x", nil)

	assert.Equal(t, http.StatusNotFound, status, "the route must not exist without a signing secret")
}

func TestWire_SlackInteractionsAcceptsSignedRequest(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.SlackSigningSecret = config.Secret("slack-signing-secret")
	server := serve(t, cfg)
	body := "payload=" + `%7B%22type%22%3A%22block_actions%22%7D`
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	status, responseBody := request(t, server, http.MethodPost, "/webhook/slack/interactions", body, map[string]string{
		security.SlackSignatureHeader: slackSignature("slack-signing-secret", timestamp, body),
		security.SlackTimestampHeader: timestamp,
		"Content-Type":                "application/x-www-form-urlencoded",
	})

	assert.Equal(t, http.StatusOK, status, "body: %s", responseBody)
}

func TestWire_SlackInteractionsRejectsForgedRequest(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.SlackSigningSecret = config.Secret("slack-signing-secret")
	server := serve(t, cfg)

	status, _ := request(t, server, http.MethodPost, "/webhook/slack/interactions", "payload=x", map[string]string{
		security.SlackSignatureHeader: "v0=deadbeef",
		security.SlackTimestampHeader: strconv.FormatInt(time.Now().Unix(), 10),
	})

	assert.Equal(t, http.StatusUnauthorized, status)
}
