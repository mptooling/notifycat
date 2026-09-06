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
	link, err := headlineLink(content)
	if err != nil {
		return RawMessageContent{}, err
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
	fallback := fmt.Sprintf("%s please review %s", movedNote, linkLabel(link))
	return RawMessageContent{Blocks: blocks, Fallback: fallback}, nil
}

// headlineLink returns the pull request link from a message's headline section.
func headlineLink(content RawMessageContent) (string, error) {
	if len(content.Blocks) == 0 {
		return "", fmt.Errorf("%w: no blocks", ErrUnexpectedMessageShape)
	}
	headline, err := sectionText(content.Blocks[0])
	if err != nil {
		return "", err
	}
	link := prLink.FindString(headline)
	if link == "" {
		return "", fmt.Errorf("%w: headline carries no pull request link", ErrUnexpectedMessageShape)
	}
	return link, nil
}

// linkLabel is a link's display text ("PR #7: Add widgets"), used for the
// mention-free push-preview text.
func linkLabel(link string) string {
	label := strings.Trim(link, "<>")
	if _, after, found := strings.Cut(label, "|"); found {
		label = after
	}
	return label
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

// MovedPointer rewrites the original message into a one-line pointer at the
// channel its replacement now lives in: the PR link struck through, the
// destination linked, and nothing else. The context line, review markers and
// the Start review button are dropped — the button would decorate a message
// that is no longer the PR's, and the detail now lives in the new channel.
func MovedPointer(content RawMessageContent, toChannel string) (Message, error) {
	link, err := headlineLink(content)
	if err != nil {
		return Message{}, err
	}
	return Message{
		Blocks:   []Block{section(fmt.Sprintf(":%s: [moved to <#%s>] ~%s~", movedEmoji, toChannel, link))},
		Fallback: fmt.Sprintf("%s %s", movedNote, linkLabel(link)),
	}, nil
}
