package mappings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Entry is one validation unit: an explicit (org, repo) pair or an
// (org, "*") wildcard. Each entry has its own hash in mappings.lock.
type Entry struct {
	Org      string
	Repo     string // empty when Wildcard is true
	Wildcard bool
	Channel  string
	Mentions []string
}

// Key returns the lock-file key for the entry: "org/repo" or "org/*".
func (e Entry) Key() string {
	if e.Wildcard {
		return e.Org + "/*"
	}
	return e.Org + "/" + e.Repo
}

// Hash is the cache key for an entry: sha256 over canonical JSON of the
// validation-relevant fields. Mentions are deliberately excluded — they
// only affect message formatting at Slack-send time, not anything the
// validator checks (channel membership, bot scopes, webhook events). A
// mention edit shouldn't invalidate the entry's cache.
func (e Entry) Hash() string {
	repo := e.Repo
	if e.Wildcard {
		repo = "*"
	}
	payload := struct {
		Org     string `json:"org"`
		Repo    string `json:"repo"`
		Channel string `json:"channel"`
	}{e.Org, repo, e.Channel}
	// json.Marshal cannot fail for a fixed struct of supported types.
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
