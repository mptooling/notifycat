package application

import (
	"regexp"
	"strings"
)

// The untrusted-data envelope. All attacker-influenced fields are placed only
// inside it; the system prompt declares everything between the markers
// data-never-instructions. Marker collisions inside content are neutralized
// before wrapping.
const (
	envelopeBegin = "<<<UNTRUSTED_DATA_BEGIN>>>"
	envelopeEnd   = "<<<UNTRUSTED_DATA_END>>>"
)

// tripwirePatterns are "ignore previous instructions"-class heuristics. A hit
// does not refuse the event — the advisor routes that one event to the
// deterministic path with guard_tripped and logs it.
var tripwirePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+|any\s+|the\s+)?(previous|prior|above|earlier)\s+instructions`),
	regexp.MustCompile(`(?i)disregard\s+(all|any|the|previous|prior|above|earlier)`),
	regexp.MustCompile(`(?i)system\s+prompt`),
	regexp.MustCompile(`(?i)you\s+are\s+now\b`),
	regexp.MustCompile(`(?i)new\s+instructions\s*:`),
	regexp.MustCompile(`(?i)UNTRUSTED_DATA_(BEGIN|END)`),
}

// guardTripped reports whether any attacker-influenced field trips an
// injection heuristic.
func guardTripped(fields ...string) bool {
	for _, field := range fields {
		for _, pattern := range tripwirePatterns {
			if pattern.MatchString(field) {
				return true
			}
		}
	}
	return false
}

// wrapUntrusted places content inside the data envelope, defanging marker
// collisions with lookalike runes.
func wrapUntrusted(content string) string {
	content = strings.ReplaceAll(content, "<<<", "‹‹‹")
	content = strings.ReplaceAll(content, ">>>", "›››")
	return envelopeBegin + "\n" + content + "\n" + envelopeEnd
}
