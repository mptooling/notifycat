package mappings

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
