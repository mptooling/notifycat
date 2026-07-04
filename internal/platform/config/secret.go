package config

import "log/slog"

// Secret wraps a sensitive string (API token, webhook secret, password).
//
// Its zero value is the empty string. Stringer, formatting verbs, and slog
// rendering all return a redaction placeholder so the raw value never reaches
// logs by accident. Use Reveal when handing the value to an external system.
type Secret string

const redacted = "[REDACTED]"

// String returns a placeholder so fmt and stringer-based logging never expose
// the underlying value. An empty Secret renders as "" to keep absence
// distinguishable from presence in operator output.
func (s Secret) String() string {
	if s == "" {
		return ""
	}
	return redacted
}

// GoString satisfies fmt's %#v verb without leaking.
func (s Secret) GoString() string {
	return s.String()
}

// Format intercepts every Sprintf verb (%v, %s, %q, %+v, …) so we cannot leak
// the raw value through an unfamiliar verb.
func (s Secret) Format(f fmtState, _ rune) {
	_, _ = f.Write([]byte(s.String()))
}

// LogValue ensures slog renders Secret with the same redaction placeholder.
func (s Secret) LogValue() slog.Value {
	return slog.StringValue(s.String())
}

// Reveal returns the underlying raw value. Call this only when passing the
// secret to an external system that needs it (HMAC, HTTP Authorization
// header, …). Never log the return value.
func (s Secret) Reveal() string {
	return string(s)
}

// fmtState is the small subset of fmt.State we need; defined as an interface
// so the config package does not need to import fmt in production code paths
// other than this single seam.
type fmtState interface {
	Write(b []byte) (n int, err error)
}
