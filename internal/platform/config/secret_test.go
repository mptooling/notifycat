package config_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/platform/config"
)

const rawSecret = "super-sensitive-value"

func TestSecret_StringRedacts(t *testing.T) {
	secret := config.Secret(rawSecret)

	assert.NotContains(t, secret.String(), rawSecret)
}

func TestSecret_EmptyStringIsEmpty(t *testing.T) {
	secret := config.Secret("")

	assert.Empty(t, secret.String())
}

func TestSecret_RevealReturnsRaw(t *testing.T) {
	secret := config.Secret("xoxb-token-value")

	assert.Equal(t, "xoxb-token-value", secret.Reveal())
}

func TestSecret_DoesNotLeakViaSlog(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	logger.Info("config loaded", "token", config.Secret(rawSecret))

	assert.NotContains(t, logged.String(), rawSecret)
}

func TestSecret_FmtVerbsDoNotLeak(t *testing.T) {
	secret := config.Secret(rawSecret)

	for _, verb := range []string{"%v", "%s", "%q", "%+v"} {
		t.Run(verb, func(t *testing.T) {
			assert.NotContains(t, stringWithVerb(verb, secret), rawSecret)
		})
	}
}
