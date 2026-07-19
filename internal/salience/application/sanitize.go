package application

import (
	"regexp"
	"strings"
)

var (
	slackMentionPattern  = regexp.MustCompile(`<[@!][^>]*>`)
	slackLinkPattern     = regexp.MustCompile(`<https?://[^>]*>`)
	bareURLPattern       = regexp.MustCompile(`https?://\S+`)
	atKeywordPattern     = regexp.MustCompile(`@(here|channel|everyone)`)
	whitespaceRunPattern = regexp.MustCompile(`\s+`)
)

// sanitizeLine makes a model-authored text field safe for a Slack message:
// mention syntax and ping keywords are stripped (the model can never mint a
// ping), URLs are stripped (the PR's own link already lives in the headline),
// mrkdwn control characters are escaped, whitespace collapses to single
// spaces on one line, and the length is capped in runes.
//
// The atKeywordPattern replacement loops to a fixed point because a single
// pass is vulnerable to reassembly: "@he@herere" becomes "@here" after one
// replacement. Looping until stable eliminates all such nested constructs.
func sanitizeLine(s string, maxRunes int) string {
	s = slackMentionPattern.ReplaceAllString(s, "")
	s = slackLinkPattern.ReplaceAllString(s, "")
	s = bareURLPattern.ReplaceAllString(s, "")
	for {
		replaced := atKeywordPattern.ReplaceAllString(s, "")
		if replaced == s {
			break
		}
		s = replaced
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = whitespaceRunPattern.ReplaceAllString(s, " ")
	return truncateRunes(strings.TrimSpace(s), maxRunes)
}
