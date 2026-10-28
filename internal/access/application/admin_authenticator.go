package application

import (
	"context"
	"log/slog"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// AdminAuthenticator implements domain.AdminAuthenticator.
type AdminAuthenticator struct {
	settings domain.Settings
	hasher   domain.Hasher
	logger   *slog.Logger
}

var _ domain.AdminAuthenticator = (*AdminAuthenticator)(nil)

// NewAdminAuthenticator builds an AdminAuthenticator.
func NewAdminAuthenticator(settings domain.Settings, hasher domain.Hasher, logger *slog.Logger) *AdminAuthenticator {
	return &AdminAuthenticator{settings: settings, hasher: hasher, logger: logger}
}

// Authenticate implements domain.AdminAuthenticator.
func (authenticator *AdminAuthenticator) Authenticate(ctx context.Context, rawKey string) error {
	if rawKey == "" || !authenticator.hasher.Matches(authenticator.settings.AdminKeyHash, rawKey) {
		authenticator.logger.WarnContext(ctx, "admin auth failed")
		return domain.ErrUnauthorized
	}
	return nil
}
