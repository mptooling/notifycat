package infrastructure

import (
	"golang.org/x/crypto/bcrypt"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// BcryptHasher is the bcrypt Hasher.
type BcryptHasher struct{}

var _ domain.Hasher = (*BcryptHasher)(nil)

// NewBcryptHasher returns a BcryptHasher at the default cost.
func NewBcryptHasher() *BcryptHasher { return &BcryptHasher{} }

// Hash implements Hasher.
func (*BcryptHasher) Hash(secret string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// Matches implements Hasher.
func (*BcryptHasher) Matches(hash, secret string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) == nil
}
