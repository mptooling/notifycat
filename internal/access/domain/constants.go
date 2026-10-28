package domain

import "time"

// Credential wire format, sizes and lifetimes.
const (
	TokenPrefix         = "nct_"
	InvitePrefix        = "nci_"
	AdminKeyPrefix      = "nca_"
	CredentialSeparator = "_"

	IDBytes             = 16
	SecretBytes         = 32
	EncodedIDLength     = 2 * IDBytes
	EncodedSecretLength = 43

	InviteTTL          = time.Hour
	LastUsedResolution = time.Minute

	UserLabelPattern = "^[a-z0-9][a-z0-9._-]{0,63}$"

	BearerScheme = "Bearer"
	InviteScheme = "Invite"

	RedeemPath          = "/api/v1/invites/redeem"
	RedeemPattern       = "POST " + RedeemPath
	CreateInvitePattern = "POST /admin/v1/invites"
	ListTokensPattern   = "GET /admin/v1/tokens"         //nolint:gosec // route pattern, not a credential
	DeleteTokenPattern  = "DELETE /admin/v1/tokens/{id}" //nolint:gosec // route pattern, not a credential
	TokenIDPathValue    = "id"

	MaxRequestBodyBytes = 16 << 10

	PublicURLVariable = "$NOTIFYCAT_URL"
)
