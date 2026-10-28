package infrastructure

import (
	"context"
	"errors"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// InviteRepo adapts persistence.AccessInvites to domain.InviteStore.
type InviteRepo struct {
	invites *persistence.AccessInvites
}

// NewInviteRepo wraps the invites repository.
func NewInviteRepo(invites *persistence.AccessInvites) *InviteRepo {
	return &InviteRepo{invites: invites}
}

// Create implements domain.InviteStore.
func (r *InviteRepo) Create(ctx context.Context, invite domain.StoredInvite) error {
	return r.invites.Create(ctx, persistence.AccessInvite{
		ID:         invite.ID,
		User:       invite.User,
		SecretHash: invite.SecretHash,
		ExpiresAt:  invite.ExpiresAt,
		UsedAt:     invite.UsedAt,
		CreatedAt:  invite.CreatedAt,
	})
}

// Find implements domain.InviteStore.
func (r *InviteRepo) Find(ctx context.Context, id string) (domain.StoredInvite, error) {
	row, err := r.invites.Find(ctx, id)
	if errors.Is(err, persistence.ErrNotFound) {
		return domain.StoredInvite{}, domain.ErrInviteNotFound
	}
	if err != nil {
		return domain.StoredInvite{}, err
	}
	return domain.StoredInvite{
		Invite: domain.Invite{
			ID:        row.ID,
			User:      row.User,
			ExpiresAt: row.ExpiresAt,
			UsedAt:    row.UsedAt,
			CreatedAt: row.CreatedAt,
		},
		SecretHash: row.SecretHash,
	}, nil
}

// Redeem implements domain.InviteStore.
func (r *InviteRepo) Redeem(ctx context.Context, inviteID string, token domain.StoredToken) error {
	err := r.invites.Redeem(ctx, inviteID, persistence.AccessToken{
		ID:         token.ID,
		User:       token.User,
		SecretHash: token.SecretHash,
		CreatedAt:  token.CreatedAt,
		LastUsedAt: token.LastUsedAt,
	})
	if errors.Is(err, persistence.ErrInviteUsed) {
		return domain.ErrInviteUsed
	}
	return err
}

var _ domain.InviteStore = (*InviteRepo)(nil)
