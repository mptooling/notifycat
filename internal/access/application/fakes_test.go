package application_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/mptooling/notifycat/internal/access/domain"
)

var (
	now          = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	inviteID     = "0123456789abcdef0123456789abcdef"
	tokenID      = "fedcba9876543210fedcba9876543210"
	inviteSecret = strings.Repeat("A", 43)
	tokenSecret  = strings.Repeat("B", 42) + "A"
)

type redeemCall struct {
	inviteID string
	token    domain.StoredToken
}

type touchCall struct {
	id     string
	usedAt time.Time
}

type fakeInviteStore struct {
	invites        map[string]domain.StoredInvite
	created        []domain.StoredInvite
	redeemed       []redeemCall
	redeemAttempts int
	findErr        error
	redeemErr      error
}

func newFakeInviteStore() *fakeInviteStore {
	return &fakeInviteStore{invites: map[string]domain.StoredInvite{}}
}

func (store *fakeInviteStore) Create(_ context.Context, invite domain.StoredInvite) error {
	store.created = append(store.created, invite)
	store.invites[invite.ID] = invite
	return nil
}

func (store *fakeInviteStore) Find(_ context.Context, id string) (domain.StoredInvite, error) {
	if store.findErr != nil {
		return domain.StoredInvite{}, store.findErr
	}
	invite, found := store.invites[id]
	if !found {
		return domain.StoredInvite{}, domain.ErrInviteNotFound
	}
	return invite, nil
}

func (store *fakeInviteStore) Redeem(_ context.Context, id string, token domain.StoredToken) error {
	store.redeemAttempts++
	if store.redeemErr != nil {
		return store.redeemErr
	}
	store.redeemed = append(store.redeemed, redeemCall{inviteID: id, token: token})
	return nil
}

type fakeTokenStore struct {
	tokens   map[string]domain.StoredToken
	touched  []touchCall
	deleted  []string
	findErr  error
	touchErr error
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{tokens: map[string]domain.StoredToken{}}
}

func (store *fakeTokenStore) Find(_ context.Context, id string) (domain.StoredToken, error) {
	if store.findErr != nil {
		return domain.StoredToken{}, store.findErr
	}
	token, found := store.tokens[id]
	if !found {
		return domain.StoredToken{}, domain.ErrTokenNotFound
	}
	return token, nil
}

func (store *fakeTokenStore) List(_ context.Context) ([]domain.Token, error) {
	tokens := make([]domain.Token, 0, len(store.tokens))
	for _, stored := range store.tokens {
		tokens = append(tokens, stored.Token)
	}
	return tokens, nil
}

func (store *fakeTokenStore) Delete(_ context.Context, id string) error {
	if _, found := store.tokens[id]; !found {
		return domain.ErrTokenNotFound
	}
	store.deleted = append(store.deleted, id)
	delete(store.tokens, id)
	return nil
}

func (store *fakeTokenStore) TouchLastUsed(_ context.Context, id string, usedAt time.Time) error {
	if store.touchErr != nil {
		return store.touchErr
	}
	store.touched = append(store.touched, touchCall{id: id, usedAt: usedAt})
	return nil
}

type fakeSecrets struct {
	ids     []string
	secrets []string
}

func (source *fakeSecrets) NewID() (string, error) {
	id := source.ids[0]
	source.ids = source.ids[1:]
	return id, nil
}

func (source *fakeSecrets) NewSecret() (string, error) {
	secret := source.secrets[0]
	source.secrets = source.secrets[1:]
	return secret, nil
}

type fakeHasher struct {
	hashCalls  int
	matchCalls int
}

func (hasher *fakeHasher) Hash(secret string) (string, error) {
	hasher.hashCalls++
	return "hash:" + secret, nil
}

func (hasher *fakeHasher) Matches(hash, secret string) bool {
	hasher.matchCalls++
	return hash == "hash:"+secret
}

type fakeClock struct {
	now time.Time
}

func (clock *fakeClock) Now() time.Time { return clock.now }

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buffer := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(buffer, nil)), buffer
}
