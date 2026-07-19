package application

import (
	"strings"
	"testing"
)

func TestSanitizeLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"strips user mention", "ping <@U123> now", "ping now"},
		{"strips channel bang", "hey <!channel> look", "hey look"},
		{"strips at keywords", "cc @here and @channel please", "cc and please"},
		{"strips reassembled at keyword", "@he@herere", ""},
		{"strips slack links", "see <https://evil.example|click me>", "see"},
		{"strips bare urls", "go to https://evil.example/path now", "go to now"},
		{"escapes mrkdwn control chars", "a & b < c > d", "a &amp; b &lt; c &gt; d"},
		{"collapses to one line", "first\nsecond\tthird", "first second third"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeLine(tc.in, 200); got != tc.want {
				t.Errorf("sanitizeLine(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeLineCapsRunes(t *testing.T) {
	got := sanitizeLine(strings.Repeat("x", 500), 120)
	if runeCount := len([]rune(got)); runeCount > 120 {
		t.Errorf("length = %d; cap 120", runeCount)
	}
}
