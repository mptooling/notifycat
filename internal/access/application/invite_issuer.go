package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// InviteIssuer implements domain.InviteIssuer.
type InviteIssuer struct {
	invites  domain.InviteStore
	secrets  domain.SecretSource
	hasher   domain.Hasher
	clock    domain.Clock
	settings domain.Settings
	logger   *slog.Logger
}

var _ domain.InviteIssuer = (*InviteIssuer)(nil)

// NewInviteIssuer builds an InviteIssuer.
func NewInviteIssuer(params domain.InviteIssuerParams) *InviteIssuer {
	return &InviteIssuer{
		invites:  params.Invites,
		secrets:  params.Secrets,
		hasher:   params.Hasher,
		clock:    params.Clock,
		settings: params.Settings,
		logger:   params.Logger,
	}
}

// Issue implements domain.InviteIssuer.
func (issuer *InviteIssuer) Issue(ctx context.Context, user string) (domain.IssuedInvite, error) {
	if !domain.ValidUser(user) {
		return domain.IssuedInvite{}, domain.ErrInvalidUser
	}
	id, err := issuer.secrets.NewID()
	if err != nil {
		return domain.IssuedInvite{}, fmt.Errorf("generate invite id: %w", err)
	}
	secret, err := issuer.secrets.NewSecret()
	if err != nil {
		return domain.IssuedInvite{}, fmt.Errorf("generate invite secret: %w", err)
	}
	secretHash, err := issuer.hasher.Hash(secret)
	if err != nil {
		return domain.IssuedInvite{}, fmt.Errorf("hash invite secret: %w", err)
	}
	now := issuer.clock.Now()
	invite := domain.Invite{ID: id, User: user, ExpiresAt: now.Add(domain.InviteTTL), CreatedAt: now}
	if err := issuer.invites.Create(ctx, domain.StoredInvite{Invite: invite, SecretHash: secretHash}); err != nil {
		return domain.IssuedInvite{}, fmt.Errorf("store invite: %w", err)
	}
	issuer.logger.InfoContext(ctx, "agent invite created",
		"user", invite.User, "invite_id", invite.ID, "expires_at", invite.ExpiresAt)
	encoded := domain.Credential{Prefix: domain.InvitePrefix, ID: id, Secret: secret}.Encode()
	return domain.IssuedInvite{
		Invite:        invite,
		Secret:        encoded,
		RedeemCommand: domain.RedeemCommand(issuer.settings.PublicDomain, encoded),
	}, nil
}
