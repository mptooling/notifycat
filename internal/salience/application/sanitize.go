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
func sanitizeLine(s string, maxRunes int) string {
	s = slackMentionPattern.ReplaceAllString(s, "")
	s = slackLinkPattern.ReplaceAllString(s, "")
	s = bareURLPattern.ReplaceAllString(s, "")
	s = atKeywordPattern.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = whitespaceRunPattern.ReplaceAllString(s, " ")
	return truncateRunes(strings.TrimSpace(s), maxRunes)
}
