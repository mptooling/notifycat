package config_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/config"
)

func TestSecret_StringRedacts(t *testing.T) {
	s := config.Secret("super-sensitive-value")

	if got := s.String(); strings.Contains(got, "super-sensitive-value") {
		t.Fatalf("Secret.String() leaked the value: %q", got)
	}
}

func TestSecret_EmptyStringIsEmpty(t *testing.T) {
	s := config.Secret("")

	if got := s.String(); got != "" {
		t.Fatalf("Secret.String() on empty value = %q; want empty string", got)
	}
}

func TestSecret_RevealReturnsRaw(t *testing.T) {
	const raw = "xoxb-token-value"
	s := config.Secret(raw)

	if got := s.Reveal(); got != raw {
		t.Fatalf("Secret.Reveal() = %q; want %q", got, raw)
	}
}

func TestSecret_DoesNotLeakViaSlog(t *testing.T) {
	const raw = "super-sensitive-value"
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger.Info("config loaded", "token", config.Secret(raw))

	if strings.Contains(buf.String(), raw) {
		t.Fatalf("slog output leaked secret value: %q", buf.String())
	}
}

func TestSecret_FmtVerbsDoNotLeak(t *testing.T) {
	const raw = "super-sensitive-value"
	s := config.Secret(raw)

	cases := map[string]string{
		"%v": "%v",
		"%s": "%s",
		"%q": "%q",
		"%+v": "%+v",
	}
	for name, verb := range cases {
		t.Run(name, func(t *testing.T) {
			// fmt.Sprintf via the verb; we expect redaction for any of these.
			got := stringWithVerb(verb, s)
			if strings.Contains(got, raw) {
				t.Fatalf("Sprintf(%q, secret) leaked value: %q", verb, got)
			}
		})
	}
}
