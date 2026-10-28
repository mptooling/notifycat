package application_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/access/application"
	"github.com/mptooling/notifycat/internal/access/domain"
)

type redeemerFixture struct {
	redeemer *application.InviteRedeemer
	invites  *fakeInviteStore
	hasher   *fakeHasher
	clock    *fakeClock
	logged   *bytes.Buffer
}

func newRedeemerFixture() redeemerFixture {
	logger, logged := captureLogger()
	invites := newFakeInviteStore()
	invites.invites[inviteID] = domain.StoredInvite{
		Invite:     domain.Invite{ID: inviteID, User: "alice", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		SecretHash: "hash:" + inviteSecret,
	}
	hasher := &fakeHasher{}
	clock := &fakeClock{now: now}
	redeemer := application.NewInviteRedeemer(domain.InviteRedeemerParams{
		Invites: invites,
		Secrets: &fakeSecrets{ids: []string{tokenID}, secrets: []string{tokenSecret}},
		Hasher:  hasher,
		Clock:   clock,
		Logger:  logger,
	})
	return redeemerFixture{redeemer: redeemer, invites: invites, hasher: hasher, clock: clock, logged: logged}
}

func validInvite() string { return "nci_" + inviteID + "_" + inviteSecret }

func TestInviteRedeemer_Redeem_CreatesTokenAndMarksInviteUsed(t *testing.T) {
	fixture := newRedeemerFixture()

	issued, err := fixture.redeemer.Redeem(t.Context(), validInvite())

	require.NoError(t, err)
	require.Len(t, fixture.invites.redeemed, 1)
	assert.Equal(t, redeemCall{inviteID: inviteID, token: domain.StoredToken{
		Token:      domain.Token{ID: tokenID, User: "alice", CreatedAt: now},
		SecretHash: "hash:" + tokenSecret,
	}}, fixture.invites.redeemed[0])
	assert.Equal(t, "nct_"+tokenID+"_"+tokenSecret, issued.Secret)
	assert.Contains(t, fixture.logged.String(), "agent token created")
}

func TestInviteRedeemer_Redeem_ValidJustBeforeExpiry(t *testing.T) {
	fixture := newRedeemerFixture()
	fixture.clock.now = now.Add(time.Hour - time.Nanosecond)

	_, err := fixture.redeemer.Redeem(t.Context(), validInvite())

	assert.NoError(t, err)
}

func TestInviteRedeemer_Redeem_Failures(t *testing.T) {
	testCases := []struct {
		name           string
		raw            string
		arrange        func(fixture redeemerFixture)
		wantReason     domain.AuthFailureReason
		wantMatchCalls int
	}{
		{"malformed", "nci_garbage", func(redeemerFixture) {}, domain.AuthFailureMalformed, 0},
		{"unknown", validInvite(), func(fixture redeemerFixture) { fixture.invites.findErr = domain.ErrInviteNotFound }, domain.AuthFailureUnknown, 0},
		{"mismatch", validInvite(), func(fixture redeemerFixture) {
			stored := fixture.invites.invites[inviteID]
			stored.SecretHash = "hash:other"
			fixture.invites.invites[inviteID] = stored
		}, domain.AuthFailureMismatch, 1},
		{"used", validInvite(), func(fixture redeemerFixture) {
			stored := fixture.invites.invites[inviteID]
			stored.UsedAt = &now
			fixture.invites.invites[inviteID] = stored
		}, domain.AuthFailureUsed, 1},
		{"expired", validInvite(), func(fixture redeemerFixture) { fixture.clock.now = now.Add(time.Hour) }, domain.AuthFailureExpired, 1},
		{"expired later", validInvite(), func(fixture redeemerFixture) { fixture.clock.now = now.Add(time.Hour + time.Second) }, domain.AuthFailureExpired, 1},
		{"lost race", validInvite(), func(fixture redeemerFixture) { fixture.invites.redeemErr = domain.ErrInviteUsed }, domain.AuthFailureUsed, 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newRedeemerFixture()
			testCase.arrange(fixture)

			_, err := fixture.redeemer.Redeem(t.Context(), testCase.raw)

			var authErr *domain.AuthError
			require.ErrorAs(t, err, &authErr)
			assert.Equal(t, testCase.wantReason, authErr.Reason)
			assert.ErrorIs(t, err, domain.ErrUnauthorized)
			assert.Contains(t, fixture.logged.String(), "agent invite redeem failed")
			assert.Contains(t, fixture.logged.String(), "reason="+string(testCase.wantReason))
			assert.Equal(t, testCase.wantMatchCalls, fixture.hasher.matchCalls)
			if testCase.name != "lost race" {
				assert.Empty(t, fixture.invites.redeemed)
			}
		})
	}
}

func TestInviteRedeemer_Redeem_StoreErrorIsNotUnauthorized(t *testing.T) {
	fixture := newRedeemerFixture()
	fixture.invites.findErr = errors.New("disk")

	_, err := fixture.redeemer.Redeem(t.Context(), validInvite())

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrUnauthorized)
}
