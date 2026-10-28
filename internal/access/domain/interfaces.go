package domain

import (
	"context"
	"time"
)

// InviteStore persists single-use invites.
type InviteStore interface {
	Create(ctx context.Context, invite StoredInvite) error
	// Find returns ErrInviteNotFound when no invite has the id.
	Find(ctx context.Context, id string) (StoredInvite, error)
	// Redeem marks the invite used and inserts the token in one transaction; it returns ErrInviteUsed when the invite was already used.
	Redeem(ctx context.Context, inviteID string, token StoredToken) error
}

// TokenStore persists agent tokens.
type TokenStore interface {
	// Find returns ErrTokenNotFound when no token has the id.
	Find(ctx context.Context, id string) (StoredToken, error)
	List(ctx context.Context) ([]Token, error)
	// Delete returns ErrTokenNotFound when no token has the id.
	Delete(ctx context.Context, id string) error
	TouchLastUsed(ctx context.Context, id string, usedAt time.Time) error
}

// SecretSource generates random credential parts.
type SecretSource interface {
	// NewID returns IDBytes random bytes, hex encoded.
	NewID() (string, error)
	// NewSecret returns SecretBytes random bytes, base64url encoded without padding.
	NewSecret() (string, error)
}

// Hasher hashes and verifies credential secrets.
type Hasher interface {
	Hash(secret string) (string, error)
	Matches(hash, secret string) bool
}

// Clock provides the current time.
type Clock interface {
	Now() time.Time
}

// InviteIssuer creates an invite for a user.
type InviteIssuer interface {
	Issue(ctx context.Context, user string) (IssuedInvite, error)
}

// InviteRedeemer exchanges a raw invite credential for a token.
type InviteRedeemer interface {
	Redeem(ctx context.Context, rawInvite string) (IssuedToken, error)
}

// TokenAdmin lists and revokes tokens.
type TokenAdmin interface {
	List(ctx context.Context) ([]Token, error)
	Delete(ctx context.Context, id string) error
}

// TokenAuthenticator resolves a raw token credential to a Principal.
type TokenAuthenticator interface {
	Authenticate(ctx context.Context, rawToken string) (Principal, error)
}

// AdminAuthenticator verifies a raw admin key.
type AdminAuthenticator interface {
	Authenticate(ctx context.Context, rawKey string) error
}
