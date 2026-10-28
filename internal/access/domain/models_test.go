package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/access/domain"
)

func TestValidUser(t *testing.T) {
	testCases := []struct {
		name  string
		user  string
		valid bool
	}{
		{"simple", "alice", true},
		{"single char", "a", true},
		{"punctuation", "alice.smith_2-x", true},
		{"max length", "a" + strings.Repeat("b", 63), true},
		{"empty", "", false},
		{"upper-case", "Alice", false},
		{"leading dash", "-alice", false},
		{"leading dot", ".alice", false},
		{"too long", "a" + strings.Repeat("b", 64), false},
		{"space", "alice smith", false},
		{"non-ascii", "älice", false},
		{"trailing newline", "alice\n", false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.valid, domain.ValidUser(testCase.user))
		})
	}
}

func TestPrincipalFrom_ReturnsStoredPrincipal(t *testing.T) {
	principal := domain.Principal{TokenID: "t1", User: "alice"}

	got, ok := domain.PrincipalFrom(domain.WithPrincipal(context.Background(), principal))

	assert.True(t, ok)
	assert.Equal(t, principal, got)
}

func TestPrincipalFrom_EmptyContext(t *testing.T) {
	got, ok := domain.PrincipalFrom(context.Background())

	assert.False(t, ok)
	assert.Zero(t, got)
}

func TestRedeemCommand(t *testing.T) {
	assert.Equal(t,
		`curl -fsS -X POST -H 'Accept: text/plain' -H @- https://notifycat.example.com/api/v1/invites/redeem <<<'Authorization: Invite nci_x'`,
		domain.RedeemCommand("notifycat.example.com", "nci_x"))
	assert.Equal(t,
		`curl -fsS -X POST -H 'Accept: text/plain' -H @- "$NOTIFYCAT_URL/api/v1/invites/redeem" <<<'Authorization: Invite nci_x'`,
		domain.RedeemCommand("", "nci_x"))
}

func TestShellExports(t *testing.T) {
	assert.Equal(t,
		"export NOTIFYCAT_URL=https://notifycat.example.com\nexport NOTIFYCAT_TOKEN='nct_y'",
		domain.ShellExports("notifycat.example.com", "nct_y"))
	assert.Equal(t,
		"export NOTIFYCAT_TOKEN='nct_y'",
		domain.ShellExports("", "nct_y"), "without server.domain the developer keeps the URL they already set")
}

func TestAuthError_UnwrapsToUnauthorized(t *testing.T) {
	err := &domain.AuthError{Reason: domain.AuthFailureMismatch}

	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.ErrorContains(t, err, "mismatch")
}
