package app_test

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

	"github.com/mptooling/notifycat/internal/app"
	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/platform/security"
)

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

func TestWire_ReturnsServerAndCleanup(t *testing.T) {
	cfg := newTestConfig(t)

	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	defer cleanup()

	if server == nil {
		t.Fatal("Wire returned nil server")
	}
	if server.Handler == nil {
		t.Fatal("server.Handler is nil")
	}
}

func TestWire_HealthzReturns200(t *testing.T) {
	cfg := newTestConfig(t)
	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(server.Handler)
	defer ts.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/healthz", nil)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d; want 200", resp.StatusCode)
	}
}

func TestWire_RejectsUnsignedWebhook(t *testing.T) {
	cfg := newTestConfig(t)
	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(server.Handler)
	defer ts.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/webhook/github", strings.NewReader(`{"action":"opened"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", resp.StatusCode)
	}
}

func TestWire_AcceptsSignedWebhookButHasNoMapping(t *testing.T) {
	cfg := newTestConfig(t)
	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(server.Handler)
	defer ts.Close()

	body := []byte(`{
		"action": "opened",
		"repository": {"full_name": "octo/no-mapping"},
		"pull_request": {"number": 1, "title": "t", "html_url": "u", "user": {"login": "a"}}
	}`)

	mac := hmac.New(sha256.New, []byte("topsecret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/webhook/github", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, body = %s; want 200 (handler should noop)", resp.StatusCode, b)
	}
}

func TestWire_SlackInteractionsAbsentWithoutSigningSecret(t *testing.T) {
	cfg := newTestConfig(t) // no SlackSigningSecret
	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(server.Handler)
	defer ts.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/webhook/slack/interactions", strings.NewReader("payload=x"))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d; want 404 (route must be absent without a signing secret)", resp.StatusCode)
	}
}

func TestWire_SlackInteractionsAcceptsSignedRequest(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.SlackSigningSecret = config.Secret("slack-signing-secret")
	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(server.Handler)
	defer ts.Close()

	body := []byte("payload=" + `%7B%22type%22%3A%22block_actions%22%7D`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte("slack-signing-secret"))
	mac.Write([]byte("v0:" + timestamp + ":"))
	mac.Write(body)
	signature := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/webhook/slack/interactions", strings.NewReader(string(body)))
	req.Header.Set(security.SlackSignatureHeader, signature)
	req.Header.Set(security.SlackTimestampHeader, timestamp)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Errorf("status = %d, body = %s; want 200 for a correctly signed interaction", resp.StatusCode, b)
	}
}

func TestWire_SlackInteractionsRejectsForgedRequest(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.SlackSigningSecret = config.Secret("slack-signing-secret")
	server, _, _, cleanup, err := app.Wire(cfg)
	if err != nil {
		t.Fatalf("Wire: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(server.Handler)
	defer ts.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/webhook/slack/interactions", strings.NewReader("payload=x"))
	req.Header.Set(security.SlackSignatureHeader, "v0=deadbeef")
	req.Header.Set(security.SlackTimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401 for a forged signature", resp.StatusCode)
	}
}
