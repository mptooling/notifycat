package application_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/application"
	"github.com/mptooling/notifycat/internal/access/domain"
)

func newIssuer(invites *fakeInviteStore, publicDomain string) (*application.InviteIssuer, *bytes.Buffer) {
	logger, logged := captureLogger()
	issuer := application.NewInviteIssuer(domain.InviteIssuerParams{
		Invites:  invites,
		Secrets:  &fakeSecrets{ids: []string{inviteID}, secrets: []string{inviteSecret}},
		Hasher:   &fakeHasher{},
		Clock:    &fakeClock{now: now},
		Settings: domain.Settings{PublicDomain: publicDomain},
		Logger:   logger,
	})
	return issuer, logged
}

func TestInviteIssuer_Issue_StoresHashedInviteForOneHour(t *testing.T) {
	invites := newFakeInviteStore()
	issuer, _ := newIssuer(invites, "notifycat.example.com")

	issued, err := issuer.Issue(t.Context(), "alice")

	require.NoError(t, err)
	require.Len(t, invites.created, 1)
	assert.Equal(t, domain.StoredInvite{
		Invite:     domain.Invite{ID: inviteID, User: "alice", ExpiresAt: now.Add(time.Hour), CreatedAt: now},
		SecretHash: "hash:" + inviteSecret,
	}, invites.created[0])
	assert.Equal(t, "nci_"+inviteID+"_"+inviteSecret, issued.Secret)
	assert.Equal(t, now.Add(time.Hour), issued.Invite.ExpiresAt)
}

func TestInviteIssuer_Issue_RedeemCommandUsesServerDomain(t *testing.T) {
	issuer, _ := newIssuer(newFakeInviteStore(), "notifycat.example.com")

	issued, err := issuer.Issue(t.Context(), "alice")

	require.NoError(t, err)
	assert.Equal(t, "curl -fsS -X POST -H @- https://notifycat.example.com/api/v1/invites/redeem <<<'Authorization: Invite nci_"+inviteID+"_"+inviteSecret+"'", issued.RedeemCommand)
}

func TestInviteIssuer_Issue_RejectsInvalidUser(t *testing.T) {
	testCases := []struct {
		name string
		user string
	}{
		{"empty", ""},
		{"uppercase", "Alice"},
		{"leading dash", "-alice"},
		{"too long", string(bytes.Repeat([]byte("a"), 65))},
		{"space", "alice smith"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			invites := newFakeInviteStore()
			issuer, _ := newIssuer(invites, "notifycat.example.com")

			_, err := issuer.Issue(t.Context(), testCase.user)

			assert.ErrorIs(t, err, domain.ErrInvalidUser)
			assert.Empty(t, invites.created)
		})
	}
}

func TestInviteIssuer_Issue_LogsWithoutSecret(t *testing.T) {
	issuer, logged := newIssuer(newFakeInviteStore(), "notifycat.example.com")

	_, err := issuer.Issue(t.Context(), "alice")

	require.NoError(t, err)
	assert.Contains(t, logged.String(), "agent invite created")
	assert.Contains(t, logged.String(), "user=alice")
	assert.Contains(t, logged.String(), "invite_id="+inviteID)
	assert.NotContains(t, logged.String(), inviteSecret)
}
