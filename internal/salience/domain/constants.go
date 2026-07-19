package domain

import "time"

// Decision-path constants. Deliberately not configurable in v1; each can be
// promoted to a config key later without migration.
const (
	// DecisionTimeout bounds one webhook-path model call — safely inside
	// GitHub's 10 s webhook delivery window.
	DecisionTimeout = 2500 * time.Millisecond
	// DigestDecisionTimeout bounds one digest-path model call; the cron path
	// has no delivery deadline, so it can afford more.
	DigestDecisionTimeout = 10 * time.Second

	CircuitFailureThreshold = 5
	CircuitOpenDuration     = 10 * time.Minute

	CacheSize = 512
	CacheTTL  = 24 * time.Hour

	MaxTitleChars        = 200
	MaxBodyChars         = 1500
	MaxFilePaths         = 100
	MaxDigestPRs         = 30
	MaxContextBlockChars = 120
	MaxDigestNoteChars   = 120
	MaxRationaleChars    = 200
	MaxOutputTokens      = 1024
)

// CuratedEmojis extends the operator-configured reaction emojis in every
// emoji allowlist, giving the model a small expressive set beyond the
// lifecycle reactions.
var CuratedEmojis = []string{"rocket", "warning", "lock", "package", "sparkles"}
