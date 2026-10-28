package domain

import (
	"encoding/base64"
	"strings"
)

// Credential is a parsed prefix + id + secret. It has no String method so fmt and slog never print it; use Encode.
type Credential struct {
	Prefix string
	ID     string
	Secret string
}

// Encode returns the wire form of the credential.
func (credential Credential) Encode() string {
	return credential.Prefix + credential.ID + CredentialSeparator + credential.Secret
}

// ParseCredential parses raw as prefix + hex id + "_" + base64url secret. It never splits on "_" because the secret may contain it.
func ParseCredential(raw, prefix string) (Credential, error) {
	rest, found := strings.CutPrefix(raw, prefix)
	if !found || len(rest) != EncodedIDLength+len(CredentialSeparator)+EncodedSecretLength {
		return Credential{}, ErrMalformedCredential
	}
	id := rest[:EncodedIDLength]
	if !isLowerHex(id) {
		return Credential{}, ErrMalformedCredential
	}
	if rest[EncodedIDLength:EncodedIDLength+len(CredentialSeparator)] != CredentialSeparator {
		return Credential{}, ErrMalformedCredential
	}
	secret := rest[EncodedIDLength+len(CredentialSeparator):]
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(secret)
	if err != nil || len(decoded) != SecretBytes {
		return Credential{}, ErrMalformedCredential
	}
	return Credential{Prefix: prefix, ID: id, Secret: secret}, nil
}

func isLowerHex(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		isDigit := character >= '0' && character <= '9'
		isLowerLetter := character >= 'a' && character <= 'f'
		if !isDigit && !isLowerLetter {
			return false
		}
	}
	return true
}
