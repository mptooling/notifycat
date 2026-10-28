package main

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	accessinfra "github.com/mptooling/notifycat/internal/access/infrastructure"
)

type failingSecretSource struct{}

func (failingSecretSource) NewID() (string, error)     { return "", errors.New("no entropy") }
func (failingSecretSource) NewSecret() (string, error) { return "", errors.New("no entropy") }

func TestRun_PrintsKeyAndMatchingHash(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(&stdout, &stderr, accessinfra.NewRandomSecret(), accessinfra.NewBcryptHasher())

	require.Equal(t, 0, code)
	key := strings.TrimSuffix(stdout.String(), "\n")
	assert.Regexp(t, `^nca_[A-Za-z0-9_-]{43}$`, key)
	match := regexp.MustCompile(`^NOTIFYCAT_ADMIN_KEY_HASH='(\$2a\$10\$[./A-Za-z0-9]{53})'\n$`).FindStringSubmatch(stderr.String())
	require.Len(t, match, 2)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(match[1]), []byte(key)), "the printed hash verifies the printed key")
}

func TestRun_SecretFailureExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(&stdout, &stderr, failingSecretSource{}, accessinfra.NewBcryptHasher())

	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "notifycat-keygen:")
}
