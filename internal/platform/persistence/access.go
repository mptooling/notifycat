package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrInviteUsed is returned by Redeem when the invite was already redeemed.
var ErrInviteUsed = errors.New("store: access invite already used")

// AccessInvites persists one-time invites.
type AccessInvites struct {
	db *gorm.DB
}

// NewAccessInvites constructs an AccessInvites repository bound to db.
func NewAccessInvites(db *gorm.DB) *AccessInvites {
	return &AccessInvites{db: db}
}

// Create inserts an invite.
func (r *AccessInvites) Create(ctx context.Context, invite AccessInvite) error {
	if err := r.db.WithContext(ctx).Create(&invite).Error; err != nil {
		return fmt.Errorf("store: create access invite: %w", err)
	}
	return nil
}

// Find returns the invite, or ErrNotFound.
func (r *AccessInvites) Find(ctx context.Context, id string) (AccessInvite, error) {
	var invite AccessInvite
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&invite).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AccessInvite{}, ErrNotFound
	}
	if err != nil {
		return AccessInvite{}, fmt.Errorf("store: find access invite: %w", err)
	}
	return invite, nil
}

// Redeem marks the invite used and inserts the token in one transaction, or
// returns ErrInviteUsed when the invite is unknown or already used.
func (r *AccessInvites) Redeem(ctx context.Context, inviteID string, token AccessToken) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&AccessInvite{}).
			Where("id = ? AND used_at IS NULL", inviteID).
			Update("used_at", token.CreatedAt)
		if result.Error != nil {
			return fmt.Errorf("store: mark access invite used: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrInviteUsed
		}
		if err := tx.Create(&token).Error; err != nil {
			return fmt.Errorf("store: create access token: %w", err)
		}
		return nil
	})
}

// AccessTokens persists agent tokens.
type AccessTokens struct {
	db *gorm.DB
}

// NewAccessTokens constructs an AccessTokens repository bound to db.
func NewAccessTokens(db *gorm.DB) *AccessTokens {
	return &AccessTokens{db: db}
}

// Find returns the token, or ErrNotFound.
func (r *AccessTokens) Find(ctx context.Context, id string) (AccessToken, error) {
	var token AccessToken
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AccessToken{}, ErrNotFound
	}
	if err != nil {
		return AccessToken{}, fmt.Errorf("store: find access token: %w", err)
	}
	return token, nil
}

// List returns all tokens ordered by creation time.
func (r *AccessTokens) List(ctx context.Context) ([]AccessToken, error) {
	var tokens []AccessToken
	if err := r.db.WithContext(ctx).Order("created_at, id").Find(&tokens).Error; err != nil {
		return nil, fmt.Errorf("store: list access tokens: %w", err)
	}
	return tokens, nil
}

// Delete removes the token, or returns ErrNotFound.
func (r *AccessTokens) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&AccessToken{})
	if result.Error != nil {
		return fmt.Errorf("store: delete access token: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchLastUsed records when the token was last used.
func (r *AccessTokens) TouchLastUsed(ctx context.Context, id string, usedAt time.Time) error {
	err := r.db.WithContext(ctx).Model(&AccessToken{}).Where("id = ?", id).Update("last_used_at", usedAt).Error
	if err != nil {
		return fmt.Errorf("store: touch access token: %w", err)
	}
	return nil
}
