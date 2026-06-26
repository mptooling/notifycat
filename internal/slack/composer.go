// Package slack talks to the Slack Web API for the PR notifier: posting,
// updating, and deleting messages, adding emoji reactions, and composing the
// blocks of the notification.
package slack

import (
	"fmt"
	"strings"
	"time"
)

// PRDetails is the subset of PR information the Composer needs to render a
// notification. It is detached from any HTTP payload type so the composer
// stays a pure function of its inputs.
type PRDetails struct {
	Repository string
	Number     int
	Title      string
	URL        string
	Author     string
	// CreatedAt is the PR's open time, rendered as a localized date token in
	// the context line. The zero value omits the "opened …" clause.
	CreatedAt time.Time
}

// Message is a composed Slack message: the Block Kit blocks rendered in the
// channel plus a plain-text Fallback. Slack uses Fallback for the mobile push
// preview and for screen readers — it does not read interior block text for
// either — so every Message carries one.
type Message struct {
	Blocks   []Block
	Fallback string
}

// Block is the narrow subset of Block Kit the notifier emits: a "section"
// (Text set) or a "context" line (Elements set). Exactly one of the two is
// populated per block. The struct marshals directly to the Slack blocks JSON.
type Block struct {
	Type     string       `json:"type"`
	Text     *TextObject  `json:"text,omitempty"`
	Elements []TextObject `json:"elements,omitempty"`
}

// TextObject is a Block Kit text object. The notifier only ever emits mrkdwn.
type TextObject struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Composer renders Slack-formatted notification messages.
type Composer struct {
	newPREmoji string
}

// NewComposer returns a Composer that prefixes new-PR messages with the given
// reaction-style emoji name (without colons).
func NewComposer(newPREmoji string) *Composer {
	return &Composer{newPREmoji: newPREmoji}
}

// NewMessage renders the initial notification for a PR: a headline section with
// the new-PR emoji, any mentions, and the linked title, plus a muted context
// line carrying repo, author, and the localized open time. Mentions stay in the
// section because Slack only reliably notifies on a mention in a section/
// top-level text — a context block renders the mention as gray text but does
// not ping. When the mentions list is empty the prefix is omitted entirely so
// the message has no stranded ", ".
// newPREmoji is the per-repo reaction emoji name (without colons). If empty,
// falls back to the composer's default emoji.
func (c *Composer) NewMessage(pr PRDetails, mentions []string, newPREmoji string) Message {
	if newPREmoji == "" {
		newPREmoji = c.newPREmoji
	}
	headline := fmt.Sprintf(
		":%s: %splease review <%s|PR #%d: %s>",
		newPREmoji, mentionsPrefix(mentions), pr.URL, pr.Number, pr.Title,
	)
	fallback := fmt.Sprintf(
		"%splease review PR #%d: %s by %s",
		mentionsPrefix(mentions), pr.Number, pr.Title, pr.Author,
	)
	return Message{
		Blocks:   []Block{section(headline), contextBlock(contextLine(pr))},
		Fallback: fallback,
	}
}

// BotMessage renders the compact notification for a PR opened by a dependency
// bot. bot is the lowercase bot name ("dependabot" / "renovate"). When security
// is true it uses the rotating-light advisory template; otherwise the package
// routine-bump template. It stays deliberately compact — a single section, no
// context line — so bot bumps read as a one-liner. Mentions follow the same
// empty-list rule as NewMessage; the PR author is omitted because the bot name
// carries it.
func (c *Composer) BotMessage(pr PRDetails, mentions []string, bot string, security bool) Message {
	emoji, verb := "package", "bumped"
	if security {
		emoji, verb = "rotating_light", "security update"
	}
	headline := fmt.Sprintf(
		":%s: %s%s %s <%s|PR #%d: %s>",
		emoji, mentionsPrefix(mentions), bot, verb, pr.URL, pr.Number, pr.Title,
	)
	fallback := fmt.Sprintf(
		"%s%s %s PR #%d: %s",
		mentionsPrefix(mentions), bot, verb, pr.Number, pr.Title,
	)
	return Message{Blocks: []Block{section(headline)}, Fallback: fallback}
}

// UpdatedMessage renders the closed-PR decoration. Block Kit cannot wrap the
// whole prior message string the way the legacy plain-text format did, so the
// message is rebuilt from PR details: the title is struck through inside the
// section, the leading emoji is swapped to the merged/closed reaction emoji,
// and a [Merged]/[Closed] label is prepended. The context line is preserved.
func (c *Composer) UpdatedMessage(pr PRDetails, merged bool, emoji string) Message {
	label := "Closed"
	if merged {
		label = "Merged"
	}
	headline := fmt.Sprintf(
		":%s: [%s] ~<%s|PR #%d: %s>~",
		emoji, label, pr.URL, pr.Number, pr.Title,
	)
	fallback := fmt.Sprintf("[%s] PR #%d: %s by %s", label, pr.Number, pr.Title, pr.Author)
	return Message{
		Blocks:   []Block{section(headline), contextBlock(contextLine(pr))},
		Fallback: fallback,
	}
}

// stuckDigestEmoji leads every stuck-PR digest. Fixed (not config-driven) — the
// digest is a distinct, opt-out feature, not part of the per-PR reaction set.
const stuckDigestEmoji = "hourglass_flowing_sand"

// maxSectionChars is the ceiling we pack a digest section to before starting a
// new one. Slack rejects a section whose text exceeds 3000 chars with
// invalid_blocks; we stay under that with headroom for multi-byte content.
const maxSectionChars = 2900

// StuckPR is one entry in a stuck-PR digest: a PR that has seen no activity
// since before today. The PR title is intentionally absent — the store does
// not keep it — so the digest links by repository and number.
type StuckPR struct {
	Repository string
	Number     int
	URL        string
	IdleDays   int
}

// StuckDigestParent renders the static parent of a channel's stuck-PR digest: a
// headline carrying the channel's mentions (so the post actually notifies) and
// the PR count. The PR list itself is posted as a thread reply via
// StuckDigestList — keeping the channel feed to one quiet line per channel.
// Mentions follow the same empty-list rule as NewMessage.
func (c *Composer) StuckDigestParent(mentions []string, count int) Message {
	headline := fmt.Sprintf(
		":%s: %s%d open PR%s waiting for review since before today:",
		stuckDigestEmoji, mentionsPrefix(mentions), count, pluralSuffix(count),
	)
	fallback := fmt.Sprintf("%d PR%s waiting for review", count, pluralSuffix(count))
	return Message{Blocks: []Block{section(headline)}, Fallback: fallback}
}

// StuckDigestList renders the thread reply for a channel's stuck-PR digest: one
// line per stuck PR, with no headline (mentions and count live on the parent).
// The caller must pass a non-empty prs slice; an empty channel is skipped
// upstream.
//
// A busy channel can list more PRs than fit in one Block Kit section (Slack
// caps section text at 3000 chars), so the lines are packed into successive
// section blocks, each kept under maxSectionChars.
func (c *Composer) StuckDigestList(prs []StuckPR) Message {
	var blocks []Block
	var b strings.Builder
	for _, pr := range prs {
		line := fmt.Sprintf("• <%s|%s #%d> · idle %s", pr.URL, pr.Repository, pr.Number, idlePhrase(pr.IdleDays))
		if b.Len() > 0 && b.Len()+len("\n")+len(line) > maxSectionChars {
			blocks = append(blocks, section(b.String()))
			b.Reset()
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	blocks = append(blocks, section(b.String()))

	fallback := fmt.Sprintf("%d PR%s waiting for review", len(prs), pluralSuffix(len(prs)))
	return Message{Blocks: blocks, Fallback: fallback}
}

// idlePhrase renders a whole-day idle duration ("1 day" / "3 days").
func idlePhrase(days int) string {
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

// pluralSuffix returns "" for one and "s" otherwise.
func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// contextLine renders the muted "repo · author · opened <time>" line. When the
// creation time is unknown the "opened …" clause is dropped rather than
// rendering a bogus epoch date.
func contextLine(pr PRDetails) string {
	line := fmt.Sprintf("%s · %s", pr.Repository, pr.Author)
	if pr.CreatedAt.IsZero() {
		return line
	}
	return line + " · opened " + dateToken(pr.CreatedAt)
}

// dateToken builds Slack's localized date token. {date_short_pretty} renders
// "Today"/"Yesterday" when applicable (else e.g. "Jun 5"), and {time} the
// local clock time, so the line reads "opened Today at 2:04 PM" in each
// viewer's own timezone. The text after "|" is the fallback Slack shows when it
// cannot render the token.
func dateToken(t time.Time) string {
	return fmt.Sprintf(
		"<!date^%d^{date_short_pretty} at {time}|%s>",
		t.Unix(), t.Format("Jan 2, 2006 at 3:04 PM"),
	)
}

// section builds an mrkdwn section block.
func section(text string) Block {
	return Block{Type: "section", Text: &TextObject{Type: "mrkdwn", Text: text}}
}

// contextBlock builds an mrkdwn context block with a single element.
func contextBlock(text string) Block {
	return Block{Type: "context", Elements: []TextObject{{Type: "mrkdwn", Text: text}}}
}

// mentionsPrefix joins mentions with commas (the legacy PHP wire format) and
// appends ", "; an empty list yields "" so the message has no stranded ", ".
func mentionsPrefix(mentions []string) string {
	if len(mentions) == 0 {
		return ""
	}
	return strings.Join(mentions, ",") + ", "
}
