package security

// BitbucketVerifier checks Bitbucket's HMAC-SHA256 signatures against a shared
// secret. The scheme is identical to GitHub's ("sha256=<hex>" over the raw
// body); the two differ only in the header the middleware reads
// (SignatureHeaderBitbucket vs SignatureHeader), so both delegate to the same
// verification core.
type BitbucketVerifier struct {
	secret []byte
}

// NewBitbucketVerifier returns a BitbucketVerifier configured with the shared
// secret configured on the Bitbucket webhook (BITBUCKET_WEBHOOK_SECRET).
func NewBitbucketVerifier(secret string) *BitbucketVerifier {
	return &BitbucketVerifier{secret: []byte(secret)}
}

// Verify checks that signature is a valid "sha256=<hex>" HMAC of body using the
// verifier's secret. Returns ErrInvalidSignature for any mismatch; the
// comparison runs in constant time.
func (v *BitbucketVerifier) Verify(body []byte, signature string) error {
	return verifyRawBodyHMAC(v.secret, body, signature)
}

var _ SignatureVerifier = (*BitbucketVerifier)(nil)
