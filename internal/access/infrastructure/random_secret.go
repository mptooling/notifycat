package infrastructure

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"github.com/mptooling/notifycat/internal/access/domain"
)

// RandomSecret is the crypto/rand SecretSource.
type RandomSecret struct{}

var _ domain.SecretSource = (*RandomSecret)(nil)

// NewRandomSecret returns a RandomSecret.
func NewRandomSecret() *RandomSecret { return &RandomSecret{} }

// NewID implements SecretSource.
func (*RandomSecret) NewID() (string, error) {
	raw, err := randomBytes(domain.IDBytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// NewSecret implements SecretSource.
func (*RandomSecret) NewSecret() (string, error) {
	raw, err := randomBytes(domain.SecretBytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func randomBytes(length int) ([]byte, error) {
	buffer := make([]byte, length)
	if _, err := rand.Read(buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}
