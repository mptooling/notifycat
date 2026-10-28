package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// TokenAuthenticator implements domain.TokenAuthenticator.
type TokenAuthenticator struct {
	tokens domain.TokenStore
	hasher domain.Hasher
	clock  domain.Clock
	logger *slog.Logger
}

var _ domain.TokenAuthenticator = (*TokenAuthenticator)(nil)

// NewTokenAuthenticator builds a TokenAuthenticator.
func NewTokenAuthenticator(params domain.TokenAuthenticatorParams) *TokenAuthenticator {
	return &TokenAuthenticator{
		tokens: params.Tokens,
		hasher: params.Hasher,
		clock:  params.Clock,
		logger: params.Logger,
	}
}

// Authenticate implements domain.TokenAuthenticator.
func (authenticator *TokenAuthenticator) Authenticate(ctx context.Context, rawToken string) (domain.Principal, error) {
	credential, err := domain.ParseCredential(rawToken, domain.TokenPrefix)
	if err != nil {
		return domain.Principal{}, authenticator.fail(ctx, domain.AuthFailureMalformed)
	}
	token, err := authenticator.tokens.Find(ctx, credential.ID)
	if errors.Is(err, domain.ErrTokenNotFound) {
		return domain.Principal{}, authenticator.fail(ctx, domain.AuthFailureUnknown)
	}
	if err != nil {
		return domain.Principal{}, fmt.Errorf("find token: %w", err)
	}
	if !authenticator.hasher.Matches(token.SecretHash, credential.Secret) {
		return domain.Principal{}, authenticator.fail(ctx, domain.AuthFailureMismatch)
	}
	authenticator.touchLastUsed(ctx, token.Token)
	return domain.Principal{TokenID: token.ID, User: token.User}, nil
}

func (authenticator *TokenAuthenticator) touchLastUsed(ctx context.Context, token domain.Token) {
	now := authenticator.clock.Now()
	if token.LastUsedAt != nil && now.Sub(*token.LastUsedAt) < domain.LastUsedResolution {
		return
	}
	if err := authenticator.tokens.TouchLastUsed(ctx, token.ID, now); err != nil {
		authenticator.logger.WarnContext(ctx, "agent token last_used_at not updated", "token_id", token.ID, "err", err)
	}
}

func (authenticator *TokenAuthenticator) fail(ctx context.Context, reason domain.AuthFailureReason) error {
	authenticator.logger.WarnContext(ctx, "agent api auth failed", "reason", string(reason))
	return &domain.AuthError{Reason: reason}
}
