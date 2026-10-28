package domain_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/domain"
)

const (
	fixtureID = "0123456789abcdef0123456789abcdef"
)

var fixtureSecret = strings.Repeat("A", 43)

func TestParseCredential(t *testing.T) {
	validCases := []struct {
		name   string
		raw    string
		prefix string
		want   domain.Credential
	}{
		{"token", "nct_" + fixtureID + "_" + fixtureSecret, domain.TokenPrefix, domain.Credential{Prefix: domain.TokenPrefix, ID: fixtureID, Secret: fixtureSecret}},
		{"secret with underscore and dash", "nct_" + fixtureID + "_" + "_-" + strings.Repeat("A", 41), domain.TokenPrefix, domain.Credential{Prefix: domain.TokenPrefix, ID: fixtureID, Secret: "_-" + strings.Repeat("A", 41)}},
		{"invite", "nci_" + fixtureID + "_" + fixtureSecret, domain.InvitePrefix, domain.Credential{Prefix: domain.InvitePrefix, ID: fixtureID, Secret: fixtureSecret}},
	}
	for _, testCase := range validCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := domain.ParseCredential(testCase.raw, testCase.prefix)

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}

	malformedCases := []struct {
		name   string
		raw    string
		prefix string
	}{
		{"invite parsed as token", "nci_" + fixtureID + "_" + fixtureSecret, domain.TokenPrefix},
		{"empty", "", domain.TokenPrefix},
		{"prefix only", "nct_", domain.TokenPrefix},
		{"id one char short", "nct_" + fixtureID[:31] + "_" + fixtureSecret, domain.TokenPrefix},
		{"upper-case id", "nct_" + strings.ToUpper(fixtureID) + "_" + fixtureSecret, domain.TokenPrefix},
		{"non-hex id", "nct_" + "g" + fixtureID[1:] + "_" + fixtureSecret, domain.TokenPrefix},
		{"missing separator", "nct_" + fixtureID + fixtureSecret + "A", domain.TokenPrefix},
		{"secret 42 chars", "nct_" + fixtureID + "_" + fixtureSecret[:42], domain.TokenPrefix},
		{"secret 44 chars", "nct_" + fixtureID + "_" + fixtureSecret + "A", domain.TokenPrefix},
		{"padding last char", "nct_" + fixtureID + "_" + fixtureSecret[:42] + "=", domain.TokenPrefix},
		{"plus last char", "nct_" + fixtureID + "_" + fixtureSecret[:42] + "+", domain.TokenPrefix},
		{"non-canonical last char", "nct_" + fixtureID + "_" + fixtureSecret[:42] + "B", domain.TokenPrefix},
		{"trailing newline", "nct_" + fixtureID + "_" + fixtureSecret + "\n", domain.TokenPrefix},
	}
	for _, testCase := range malformedCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := domain.ParseCredential(testCase.raw, testCase.prefix)

			assert.ErrorIs(t, err, domain.ErrMalformedCredential)
		})
	}
}

func TestCredential_EncodeRoundTrips(t *testing.T) {
	credential := domain.Credential{Prefix: domain.TokenPrefix, ID: fixtureID, Secret: fixtureSecret}

	got, err := domain.ParseCredential(credential.Encode(), domain.TokenPrefix)

	require.NoError(t, err)
	assert.Equal(t, credential, got)
}

func TestEncodedSecretLength_MatchesBase64URL(t *testing.T) {
	assert.Equal(t, domain.EncodedSecretLength, base64.RawURLEncoding.EncodedLen(domain.SecretBytes))
}
