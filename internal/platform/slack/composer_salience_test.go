package slack_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/platform/slack"
)

func salienceTestPR() slack.PRDetails {
	return slack.PRDetails{
		Repository: "acme/api",
		Number:     7,
		Title:      "add rate limiter",
		URL:        "https://github.com/acme/api/pull/7",
		Author:     "alice",
		CreatedAt:  time.Unix(1750000000, 0),
	}
}

// The zero-option OpenMessage must be byte-identical to NewMessage — the
// deterministic advisor's output renders exactly today's message.
func TestOpenMessageZeroOptionsEqualsNewMessage(t *testing.T) {
	composer := slack.NewComposer("eyes")
	pr := salienceTestPR()
	mentions := []string{"<@U1>"}

	legacy := composer.NewMessage(pr, mentions, "rocket")
	viaOptions := composer.OpenMessage(pr, slack.OpenOptions{Mentions: mentions, NewPREmoji: "rocket"})

	legacyJSON, _ := json.Marshal(legacy)
	optionsJSON, _ := json.Marshal(viaOptions)
	if string(legacyJSON) != string(optionsJSON) {
		t.Errorf("OpenMessage(zero opts) != NewMessage:\n%s\n%s", legacyJSON, optionsJSON)
	}
}

func TestOpenMessageBreaking(t *testing.T) {
	composer := slack.NewComposer("eyes")
	msg := composer.OpenMessage(salienceTestPR(), slack.OpenOptions{NewPREmoji: "eyes", Breaking: true})
	headline := msg.Blocks[0].Text.Text
	want := ":eyes: :rotating_light: *breaking* — please review <https://github.com/acme/api/pull/7|PR #7: add rate limiter>"
	if headline != want {
		t.Errorf("headline = %q\nwant %q", headline, want)
	}
	if !strings.HasPrefix(msg.Fallback, "breaking — please review PR #7") {
		t.Errorf("Fallback = %q", msg.Fallback)
	}
}

func TestOpenMessageCompact(t *testing.T) {
	composer := slack.NewComposer("eyes")
	msg := composer.OpenMessage(salienceTestPR(), slack.OpenOptions{Mentions: []string{"<@U1>"}, NewPREmoji: "sparkles", Compact: true})
	if len(msg.Blocks) != 1 {
		t.Fatalf("compact message must be a single section; got %d blocks", len(msg.Blocks))
	}
	want := ":sparkles: <@U1>, alice opened <https://github.com/acme/api/pull/7|PR #7: add rate limiter>"
	if msg.Blocks[0].Text.Text != want {
		t.Errorf("headline = %q\nwant %q", msg.Blocks[0].Text.Text, want)
	}
}

func TestOpenMessageContextBlockAppended(t *testing.T) {
	composer := slack.NewComposer("eyes")
	msg := composer.OpenMessage(salienceTestPR(), slack.OpenOptions{NewPREmoji: "eyes", ContextBlock: "touches the payments hot path"})
	// blocks: headline, standard context line, decision context block, actions
	if len(msg.Blocks) != 4 {
		t.Fatalf("blocks = %d; want 4", len(msg.Blocks))
	}
	if msg.Blocks[2].Type != "context" || msg.Blocks[2].Elements[0].Text != "touches the payments hot path" {
		t.Errorf("decision context block = %+v", msg.Blocks[2])
	}
	if msg.Blocks[3].Type != "actions" {
		t.Errorf("actions row must stay last; got %q", msg.Blocks[3].Type)
	}
}

func TestStuckDigestListAttentionAndNote(t *testing.T) {
	composer := slack.NewComposer("eyes")
	msg := composer.StuckDigestList([]slack.StuckPR{
		{Repository: "acme/api", Number: 7, URL: "https://github.com/acme/api/pull/7", IdleDays: 3, Attention: true, Note: "blocks the release"},
		{Repository: "acme/web", Number: 9, URL: "https://github.com/acme/web/pull/9", IdleDays: 1},
	})
	text := msg.Blocks[0].Text.Text
	wantFirst := "• :warning: <https://github.com/acme/api/pull/7|acme/api #7> · idle 3 days — _blocks the release_"
	wantSecond := "• <https://github.com/acme/web/pull/9|acme/web #9> · idle 1 day"
	if text != wantFirst+"\n"+wantSecond {
		t.Errorf("list = %q\nwant %q", text, wantFirst+"\n"+wantSecond)
	}
}
