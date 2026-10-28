package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// InviteRedeemer implements domain.InviteRedeemer.
type InviteRedeemer struct {
	invites  domain.InviteStore
	secrets  domain.SecretSource
	hasher   domain.Hasher
	clock    domain.Clock
	settings domain.Settings
	logger   *slog.Logger
}

var _ domain.InviteRedeemer = (*InviteRedeemer)(nil)

// NewInviteRedeemer builds an InviteRedeemer.
func NewInviteRedeemer(params domain.InviteRedeemerParams) *InviteRedeemer {
	return &InviteRedeemer{
		invites:  params.Invites,
		secrets:  params.Secrets,
		hasher:   params.Hasher,
		clock:    params.Clock,
		settings: params.Settings,
		logger:   params.Logger,
	}
}

// Redeem implements domain.InviteRedeemer.
func (redeemer *InviteRedeemer) Redeem(ctx context.Context, rawInvite string) (domain.IssuedToken, error) {
	credential, err := domain.ParseCredential(rawInvite, domain.InvitePrefix)
	if err != nil {
		return domain.IssuedToken{}, redeemer.fail(ctx, domain.AuthFailureMalformed)
	}
	invite, err := redeemer.invites.Find(ctx, credential.ID)
	if errors.Is(err, domain.ErrInviteNotFound) {
		return domain.IssuedToken{}, redeemer.fail(ctx, domain.AuthFailureUnknown)
	}
	if err != nil {
		return domain.IssuedToken{}, fmt.Errorf("find invite: %w", err)
	}
	if !redeemer.hasher.Matches(invite.SecretHash, credential.Secret) {
		return domain.IssuedToken{}, redeemer.fail(ctx, domain.AuthFailureMismatch)
	}
	if invite.UsedAt != nil {
		return domain.IssuedToken{}, redeemer.fail(ctx, domain.AuthFailureUsed)
	}
	now := redeemer.clock.Now()
	if !now.Before(invite.ExpiresAt) {
		return domain.IssuedToken{}, redeemer.fail(ctx, domain.AuthFailureExpired)
	}
	return redeemer.issueToken(ctx, invite, now)
}

func (redeemer *InviteRedeemer) issueToken(ctx context.Context, invite domain.StoredInvite, now time.Time) (domain.IssuedToken, error) {
	id, err := redeemer.secrets.NewID()
	if err != nil {
		return domain.IssuedToken{}, fmt.Errorf("generate token id: %w", err)
	}
	secret, err := redeemer.secrets.NewSecret()
	if err != nil {
		return domain.IssuedToken{}, fmt.Errorf("generate token secret: %w", err)
	}
	secretHash, err := redeemer.hasher.Hash(secret)
	if err != nil {
		return domain.IssuedToken{}, fmt.Errorf("hash token secret: %w", err)
	}
	token := domain.Token{ID: id, User: invite.User, CreatedAt: now}
	err = redeemer.invites.Redeem(ctx, invite.ID, domain.StoredToken{Token: token, SecretHash: secretHash})
	if errors.Is(err, domain.ErrInviteUsed) {
		return domain.IssuedToken{}, redeemer.fail(ctx, domain.AuthFailureUsed)
	}
	if err != nil {
		return domain.IssuedToken{}, fmt.Errorf("redeem invite: %w", err)
	}
	redeemer.logger.InfoContext(ctx, "agent token created", "user", token.User, "token_id", token.ID)
	encoded := domain.Credential{Prefix: domain.TokenPrefix, ID: id, Secret: secret}.Encode()
	return domain.IssuedToken{
		Token:        token,
		Secret:       encoded,
		ShellExports: domain.ShellExports(redeemer.settings.PublicDomain, encoded),
	}, nil
}

func (redeemer *InviteRedeemer) fail(ctx context.Context, reason domain.AuthFailureReason) error {
	redeemer.logger.WarnContext(ctx, "agent invite redeem failed", "reason", string(reason))
	return &domain.AuthError{Reason: reason}
}
