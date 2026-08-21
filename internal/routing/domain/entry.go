package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/mptooling/notifycat/internal/kernel"
)

// Entry is one validation unit: an explicit (org, repo) pair or an
// (org, "*") wildcard. Each entry has its own hash in mappings.lock.
type Entry struct {
	Org      string
	Repo     string // empty when Wildcard is true
	Wildcard bool
	Channel  string
	Mentions []string
	// ExtraChannels are the distinct channels a repo can post to beyond its
	// primary Channel — extra base-list channels plus per-path channels (sorted,
	// deduped). They feed both validation (bot membership) and the entry hash, so
	// adding or repointing one re-triggers validation. Always empty for a wildcard
	// entry unless the org/* tier itself uses a channels: list.
	ExtraChannels []string
	// Provider is the deployment's git_provider (e.g. "github"). It hashes into
	// every entry so flipping the provider — under which the same org/repo names
	// point at different remote objects — revalidates the whole lock.
	Provider kernel.Provider
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
		Provider     kernel.Provider `json:"provider"`
		Org          string          `json:"org"`
		Repo         string          `json:"repo"`
		Channel      string          `json:"channel"`
		PathChannels []string        `json:"path_channels,omitempty"`
	}{e.Provider, e.Org, repo, e.Channel, e.ExtraChannels}
	// json.Marshal cannot fail for a fixed struct of supported types.
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
