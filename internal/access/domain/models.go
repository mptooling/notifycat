package domain

import (
	"context"
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

// IssuedToken carries the full encoded token credential in Secret, shown once.
type IssuedToken struct {
	Token  Token
	Secret string
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
