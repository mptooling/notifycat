package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// TokenAdmin implements domain.TokenAdmin.
type TokenAdmin struct {
	tokens domain.TokenStore
	logger *slog.Logger
}

var _ domain.TokenAdmin = (*TokenAdmin)(nil)

// NewTokenAdmin builds a TokenAdmin.
func NewTokenAdmin(tokens domain.TokenStore, logger *slog.Logger) *TokenAdmin {
	return &TokenAdmin{tokens: tokens, logger: logger}
}

// List implements domain.TokenAdmin.
func (admin *TokenAdmin) List(ctx context.Context) ([]domain.Token, error) {
	return admin.tokens.List(ctx)
}

// Delete implements domain.TokenAdmin.
func (admin *TokenAdmin) Delete(ctx context.Context, id string) error {
	token, err := admin.tokens.Find(ctx, id)
	if err != nil {
		return fmt.Errorf("find token: %w", err)
	}
	if err := admin.tokens.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	admin.logger.InfoContext(ctx, "agent token deleted", "user", token.User, "token_id", token.ID)
	return nil
}
