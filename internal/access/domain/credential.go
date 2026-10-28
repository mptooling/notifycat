package domain

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
)

// Credential is a parsed prefix + id + secret; fmt and slog render it redacted, and only Encode returns the wire form.
type Credential struct {
	Prefix string
	ID     string
	Secret string
}

// String renders the credential with the secret redacted.
func (credential Credential) String() string {
	return redactedCredential(credential.Prefix, credential.ID)
}

// Format writes the redacted form for every fmt verb.
func (credential Credential) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprint(state, credential.String())
}

// LogValue renders the redacted form in slog output.
func (credential Credential) LogValue() slog.Value {
	return slog.StringValue(credential.String())
}

func redactedCredential(prefix, id string) string {
	return prefix + id + CredentialSeparator + RedactedSecret
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
