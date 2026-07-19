package application

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

var (
	htmlCommentPattern   = regexp.MustCompile(`(?s)<!--.*?-->`)
	markdownImagePattern = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	base64BlobPattern    = regexp.MustCompile(`[A-Za-z0-9+/=]{200,}`)
	blankLinesPattern    = regexp.MustCompile(`\n{3,}`)
)

// redactionPatterns match secret-shaped strings that must never leave the
// process: forge and chat tokens, cloud keys, PEM headers, JWT triplets, long
// high-entropy hex (which also swallows commit SHAs — acceptable noise).
var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`xox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
	regexp.MustCompile(`\b[0-9a-fA-F]{40,}\b`),
}

const redactedPlaceholder = "[REDACTED]"

// redactSecrets replaces secret-shaped substrings — PR bodies occasionally
// contain leaked credentials and must not reach a third-party API.
func redactSecrets(s string) string {
	for _, pattern := range redactionPatterns {
		s = pattern.ReplaceAllString(s, redactedPlaceholder)
	}
	return s
}

// minimizeTitle redacts and caps a PR title.
func minimizeTitle(title string) string {
	return truncateRunes(redactSecrets(strings.TrimSpace(title)), domain.MaxTitleChars)
}

// minimizeBody strips the noise that dominates bot-authored bodies (HTML
// comments, badges/images, base64 blobs), redacts secrets, collapses blank
// runs, and caps the result.
func minimizeBody(body string) string {
	body = htmlCommentPattern.ReplaceAllString(body, "")
	body = markdownImagePattern.ReplaceAllString(body, "")
	body = base64BlobPattern.ReplaceAllString(body, "")
	body = redactSecrets(body)
	body = blankLinesPattern.ReplaceAllString(body, "\n\n")
	return truncateRunes(strings.TrimSpace(body), domain.MaxBodyChars)
}

// minimizeFiles caps the changed-file list, appending an "…and N more" marker.
func minimizeFiles(files []string) []string {
	if len(files) <= domain.MaxFilePaths {
		return files
	}
	capped := make([]string, domain.MaxFilePaths, domain.MaxFilePaths+1)
	copy(capped, files[:domain.MaxFilePaths])
	return append(capped, fmt.Sprintf("…and %d more", len(files)-domain.MaxFilePaths))
}

// truncateRunes caps s at max runes, marking the cut with an ellipsis.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
