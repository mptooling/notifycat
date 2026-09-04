package slack

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A relocated message announces itself as moved and pings nobody: the operator
// changed a routing target, which is not news worth a notification, and the
// original mentions belong to the channel the message came from. The emoji is
// fixed rather than config-driven for the same reason the digest's is — a move
// is not part of the per-repo reaction set.
const (
	movedEmoji = "truck"
	movedNote  = "[moved from another channel]"
)

// ErrUnexpectedMessageShape reports a message whose headline could not be
// rewritten. Rewriting is refused rather than guessed at: a headline we cannot
// parse would carry its original mentions into the new channel.
var ErrUnexpectedMessageShape = errors.New("slack: unexpected message shape")

// prLink matches the headline's link to the pull request — the only part of the
// original headline a moved message keeps.
var prLink = regexp.MustCompile(`<https?://[^>]+>`)

// MovedMessage rewrites a message for reposting in another channel: the
// headline becomes a mention-free "moved" line built around the original PR
// link, and every other block (context line, review markers, the Start review
// button) is carried over verbatim.
func MovedMessage(content RawMessageContent) (RawMessageContent, error) {
	if len(content.Blocks) == 0 {
		return RawMessageContent{}, fmt.Errorf("%w: no blocks", ErrUnexpectedMessageShape)
	}
	headline, err := sectionText(content.Blocks[0])
	if err != nil {
		return RawMessageContent{}, err
	}
	link := prLink.FindString(headline)
	if link == "" {
		return RawMessageContent{}, fmt.Errorf("%w: headline carries no pull request link", ErrUnexpectedMessageShape)
	}

	moved, err := json.Marshal(Block{
		Type: "section",
		Text: &TextObject{
			Type: "mrkdwn",
			Text: fmt.Sprintf(":%s: %s please review %s", movedEmoji, movedNote, link),
		},
	})
	if err != nil {
		return RawMessageContent{}, fmt.Errorf("slack: render moved headline: %w", err)
	}

	blocks := make([]json.RawMessage, 0, len(content.Blocks))
	blocks = append(blocks, moved)
	blocks = append(blocks, content.Blocks[1:]...)
	return RawMessageContent{Blocks: blocks, Fallback: movedFallback(link)}, nil
}

// movedFallback builds the push-preview text from the link's label ("PR #7:
// Add widgets"), so it stays mention-free like the headline.
func movedFallback(link string) string {
	label := strings.Trim(link, "<>")
	if _, after, found := strings.Cut(label, "|"); found {
		label = after
	}
	return fmt.Sprintf("%s please review %s", movedNote, label)
}

// sectionText returns a section block's mrkdwn text.
func sectionText(block json.RawMessage) (string, error) {
	var decoded struct {
		Type string      `json:"type"`
		Text *TextObject `json:"text"`
	}
	if err := json.Unmarshal(block, &decoded); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnexpectedMessageShape, err)
	}
	if decoded.Type != "section" || decoded.Text == nil {
		return "", fmt.Errorf("%w: headline is a %q block", ErrUnexpectedMessageShape, decoded.Type)
	}
	return decoded.Text.Text, nil
}
