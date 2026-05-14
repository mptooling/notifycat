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
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/app"
	"github.com/mptooling/notifycat/internal/config"
)

func newTestConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Addr:        ":0",
		LogLevel:    "error",
		LogFormat:   "text",
		DatabaseURL: "file:" + filepath.Join(t.TempDir(), "wire.db"),

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

	server, cleanup, err := app.Wire(cfg)
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
	server, cleanup, err := app.Wire(cfg)
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
	server, cleanup, err := app.Wire(cfg)
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
	server, cleanup, err := app.Wire(cfg)
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
