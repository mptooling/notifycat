package domain

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"time"
)

// Principal is the authenticated agent behind a request.
type Principal struct {
	TokenID string
	User    string
}

// Invite is a single-use, expiring offer to create a token.
type Invite struct {
	ID        string
	User      string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// StoredInvite is an Invite with its secret hash.
type StoredInvite struct {
	Invite
	SecretHash string
}

// Token is an agent token without its secret.
type Token struct {
	ID         string
	User       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// StoredToken is a Token with its secret hash.
type StoredToken struct {
	Token
	SecretHash string
}

// IssuedInvite carries the full encoded invite credential in Secret, shown once.
type IssuedInvite struct {
	Invite        Invite
	Secret        string
	RedeemCommand string
}

// String renders the invite with the secret redacted.
func (issued IssuedInvite) String() string {
	return redactedCredential(InvitePrefix, issued.Invite.ID)
}

// Format writes the redacted form for every fmt verb.
func (issued IssuedInvite) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprint(state, issued.String())
}

// LogValue renders the redacted form in slog output.
func (issued IssuedInvite) LogValue() slog.Value {
	return slog.StringValue(issued.String())
}

// IssuedToken carries the full encoded token credential in Secret, shown once.
type IssuedToken struct {
	Token  Token
	Secret string
}

// String renders the token with the secret redacted.
func (issued IssuedToken) String() string {
	return redactedCredential(TokenPrefix, issued.Token.ID)
}

// Format writes the redacted form for every fmt verb.
func (issued IssuedToken) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprint(state, issued.String())
}

// LogValue renders the redacted form in slog output.
func (issued IssuedToken) LogValue() slog.Value {
	return slog.StringValue(issued.String())
}

// Settings is the access configuration.
type Settings struct {
	AdminKeyHash string
	PublicDomain string
}

type principalContextKey struct{}

var userLabelExpression = regexp.MustCompile(UserLabelPattern)

// WithPrincipal stores the principal in the context.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFrom reads the principal stored by WithPrincipal.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// ValidUser reports whether user matches UserLabelPattern.
func ValidUser(user string) bool {
	return userLabelExpression.MatchString(user)
}

// RedeemCommand returns the curl command that redeems the invite.
func RedeemCommand(publicDomain, invite string) string {
	redeemURL := `"` + PublicURLVariable + RedeemPath + `"`
	if publicDomain != "" {
		redeemURL = "https://" + publicDomain + RedeemPath
	}
	return "curl -fsS -X POST -H @- " + redeemURL + " <<<'Authorization: " + InviteScheme + " " + invite + "'"
}

// InviteIssuerParams are the dependencies of the invite issuer.
type InviteIssuerParams struct {
	Invites  InviteStore
	Secrets  SecretSource
	Hasher   Hasher
	Clock    Clock
	Settings Settings
	Logger   *slog.Logger
}

// InviteRedeemerParams are the dependencies of the invite redeemer.
type InviteRedeemerParams struct {
	Invites InviteStore
	Secrets SecretSource
	Hasher  Hasher
	Clock   Clock
	Logger  *slog.Logger
}

// TokenAuthenticatorParams are the dependencies of the token authenticator.
type TokenAuthenticatorParams struct {
	Tokens TokenStore
	Hasher Hasher
	Clock  Clock
	Logger *slog.Logger
}

// AdminHandlerParams are the dependencies of the admin HTTP handler.
type AdminHandlerParams struct {
	Authenticator AdminAuthenticator
	Invites       InviteIssuer
	Tokens        TokenAdmin
	Logger        *slog.Logger
}
