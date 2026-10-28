package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
)

// TokenRepo adapts persistence.AccessTokens to domain.TokenStore.
type TokenRepo struct {
	tokens *persistence.AccessTokens
}

// NewTokenRepo wraps the tokens repository.
func NewTokenRepo(tokens *persistence.AccessTokens) *TokenRepo {
	return &TokenRepo{tokens: tokens}
}

// Find implements domain.TokenStore.
func (r *TokenRepo) Find(ctx context.Context, id string) (domain.StoredToken, error) {
	row, err := r.tokens.Find(ctx, id)
	if err != nil {
		return domain.StoredToken{}, mapTokenError(err)
	}
	return domain.StoredToken{Token: tokenFromRow(row), SecretHash: row.SecretHash}, nil
}

// List implements domain.TokenStore and never exposes a hash.
func (r *TokenRepo) List(ctx context.Context) ([]domain.Token, error) {
	rows, err := r.tokens.List(ctx)
	if err != nil {
		return nil, err
	}
	listed := make([]domain.Token, len(rows))
	for i, row := range rows {
		listed[i] = tokenFromRow(row)
	}
	return listed, nil
}

// Delete implements domain.TokenStore.
func (r *TokenRepo) Delete(ctx context.Context, id string) error {
	return mapTokenError(r.tokens.Delete(ctx, id))
}

// TouchLastUsed implements domain.TokenStore.
func (r *TokenRepo) TouchLastUsed(ctx context.Context, id string, usedAt time.Time) error {
	return r.tokens.TouchLastUsed(ctx, id, usedAt)
}

func tokenFromRow(row persistence.AccessToken) domain.Token {
	return domain.Token{ID: row.ID, User: row.User, CreatedAt: row.CreatedAt, LastUsedAt: row.LastUsedAt}
}

func mapTokenError(err error) error {
	if errors.Is(err, persistence.ErrNotFound) {
		return domain.ErrTokenNotFound
	}
	return err
}

var _ domain.TokenStore = (*TokenRepo)(nil)
