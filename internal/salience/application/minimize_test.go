package application

import (
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		leak string
	}{
		{"github pat", "token ghp_abcdefghijklmnopqrstuvwxyz123456 leaked", "ghp_"},
		{"github fine-grained", "github_pat_11ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", "github_pat_"},
		{"slack bot token", "xoxb-EXAMPLE0FAKE0TOKEN0", "xoxb-"},
		{"aws key", "AKIAIOSFODNN7EXAMPLE", "AKIA"},
		{"pem header", "-----BEGIN RSA PRIVATE KEY-----", "PRIVATE KEY"},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "eyJ"},
		{"long hex", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", "deadbeef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.in)
			if strings.Contains(got, tc.leak) {
				t.Errorf("redactSecrets(%q) = %q; still contains %q", tc.in, got, tc.leak)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("redactSecrets(%q) = %q; no redaction placeholder", tc.in, got)
			}
		})
	}
}

func TestMinimizeBodyStripsDependabotNoise(t *testing.T) {
	body := "Bumps lib from 1 to 2.\n<!-- release notes\n" + strings.Repeat("noise\n", 400) + "-->\n![badge](https://img.shields.io/x.svg)\nDetails."
	got := minimizeBody(body)
	if strings.Contains(got, "noise") || strings.Contains(got, "shields.io") {
		t.Errorf("comments/badges survived: %q", got)
	}
	if !strings.Contains(got, "Bumps lib from 1 to 2.") || !strings.Contains(got, "Details.") {
		t.Errorf("real content lost: %q", got)
	}
}

func TestMinimizeBodyCapsRunes(t *testing.T) {
	got := minimizeBody(strings.Repeat("é", domain.MaxBodyChars+500))
	if runeCount := len([]rune(got)); runeCount > domain.MaxBodyChars {
		t.Errorf("body length = %d runes; cap is %d", runeCount, domain.MaxBodyChars)
	}
}

func TestMinimizeFilesCapsWithMarker(t *testing.T) {
	files := make([]string, domain.MaxFilePaths+25)
	for i := range files {
		files[i] = "file.go"
	}
	got := minimizeFiles(files)
	if len(got) != domain.MaxFilePaths+1 {
		t.Fatalf("len = %d; want cap+marker", len(got))
	}
	if got[domain.MaxFilePaths] != "…and 25 more" {
		t.Errorf("marker = %q", got[domain.MaxFilePaths])
	}
}
