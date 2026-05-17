package mappings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
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

// Hash is the cache key for an entry: sha256 over canonical JSON, with
// mentions sorted so reordering in YAML is a no-op for the cache.
func (e Entry) Hash() string {
	mentions := append([]string(nil), e.Mentions...)
	sort.Strings(mentions)
	repo := e.Repo
	if e.Wildcard {
		repo = "*"
	}
	payload := struct {
		Org      string   `json:"org"`
		Repo     string   `json:"repo"`
		Channel  string   `json:"channel"`
		Mentions []string `json:"mentions"`
	}{e.Org, repo, e.Channel, mentions}
	b, _ := json.Marshal(payload) //nolint:errcheck // marshaling fixed struct cannot fail
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
