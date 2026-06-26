package slack_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/slack"
)

// created is a fixed PR creation time used across composer tests. Its localized
// fallback ("Jun 5, 2026 at 2:04 PM") is what Slack shows when a client cannot
// render the date token.
var created = time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC)

func sectionText(t *testing.T, m slack.Message) string {
	t.Helper()
	for _, b := range m.Blocks {
		if b.Type == "section" && b.Text != nil {
			if b.Text.Type != "mrkdwn" {
				t.Errorf("section text type = %q, want mrkdwn", b.Text.Type)
			}
			return b.Text.Text
		}
	}
	t.Fatalf("no section block in %+v", m)
	return ""
}

func contextText(t *testing.T, m slack.Message) (string, bool) {
	t.Helper()
	for _, b := range m.Blocks {
		if b.Type == "context" && len(b.Elements) > 0 {
			return b.Elements[0].Text, true
		}
	}
	return "", false
}

// wantContext builds the expected context line for a PR opened at `created`.
func wantContext(repo, author string) string {
	return fmt.Sprintf("%s · %s · opened <!date^%d^{date_short_pretty} at {time}|Jun 5, 2026 at 2:04 PM>",
		repo, author, created.Unix())
}

func TestComposer_NewMessage(t *testing.T) {
	c := slack.NewComposer("eyes")

	got := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget",
		Number:     42,
		Title:      "fix the thing",
		URL:        "https://github.com/octo/widget/pull/42",
		Author:     "alice",
		CreatedAt:  created,
	}, []string{"@bob", "@carol"}, "rocket")

	wantSection := ":rocket: @bob,@carol, please review <https://github.com/octo/widget/pull/42|PR #42: fix the thing>"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section =\n  %q\nwant\n  %q", s, wantSection)
	}
	ctx, ok := contextText(t, got)
	if !ok {
		t.Fatalf("NewMessage has no context block: %+v", got)
	}
	if want := wantContext("octo/widget", "alice"); ctx != want {
		t.Errorf("context =\n  %q\nwant\n  %q", ctx, want)
	}
	wantFallback := "@bob,@carol, please review PR #42: fix the thing by alice"
	if got.Fallback != wantFallback {
		t.Errorf("fallback =\n  %q\nwant\n  %q", got.Fallback, wantFallback)
	}
}

func TestComposer_NewMessage_NoMentions(t *testing.T) {
	c := slack.NewComposer("eyes")

	got := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a", CreatedAt: created,
	}, nil, "rocket")

	wantSection := ":rocket: please review <u|PR #1: t>"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section = %q, want %q", s, wantSection)
	}
	if strings.Contains(got.Fallback, " ,") || strings.HasPrefix(got.Fallback, ", ") {
		t.Errorf("stranded comma in fallback %q", got.Fallback)
	}
}

func TestComposer_NewMessage_ChannelFallback(t *testing.T) {
	c := slack.NewComposer("eyes")

	got := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a", CreatedAt: created,
	}, []string{"<!channel>"}, "rocket")

	wantSection := ":rocket: <!channel>, please review <u|PR #1: t>"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section = %q, want %q", s, wantSection)
	}
}

func TestComposer_NewMessage_NoCreatedAt(t *testing.T) {
	c := slack.NewComposer("eyes")

	got := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a",
	}, nil, "rocket")

	ctx, ok := contextText(t, got)
	if !ok {
		t.Fatalf("expected a context block even without created time: %+v", got)
	}
	if want := "octo/widget · a"; ctx != want {
		t.Errorf("context without created time = %q, want %q", ctx, want)
	}
	if strings.Contains(ctx, "opened") {
		t.Errorf("context should omit 'opened' when created time is zero: %q", ctx)
	}
}

func TestComposer_BotMessage_Routine(t *testing.T) {
	c := slack.NewComposer("eyes")

	got := c.BotMessage(slack.PRDetails{
		Number: 42,
		Title:  "bump acme/lib from 1.2.0 to 1.2.1",
		URL:    "https://github.com/octo/widget/pull/42",
	}, []string{"@bob"}, "dependabot", false)

	wantSection := ":package: @bob, dependabot bumped <https://github.com/octo/widget/pull/42|PR #42: bump acme/lib from 1.2.0 to 1.2.1>"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section =\n  %q\nwant\n  %q", s, wantSection)
	}
	if _, ok := contextText(t, got); ok {
		t.Errorf("bot message must stay compact (no context line): %+v", got)
	}
	if strings.Contains(got.Fallback, "please review") {
		t.Errorf("routine bot fallback should not say 'please review': %q", got.Fallback)
	}
}

func TestComposer_BotMessage_Security(t *testing.T) {
	c := slack.NewComposer("eyes")

	got := c.BotMessage(slack.PRDetails{
		Number: 42,
		Title:  "bump acme/lib from 1.2.0 to 1.2.1",
		URL:    "https://github.com/octo/widget/pull/42",
	}, []string{"@bob", "@carol"}, "renovate", true)

	wantSection := ":rotating_light: @bob,@carol, renovate security update <https://github.com/octo/widget/pull/42|PR #42: bump acme/lib from 1.2.0 to 1.2.1>"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section =\n  %q\nwant\n  %q", s, wantSection)
	}
}

func TestComposer_BotMessage_NoMentions(t *testing.T) {
	c := slack.NewComposer("eyes")

	got := c.BotMessage(slack.PRDetails{
		Number: 1, Title: "t", URL: "u",
	}, nil, "dependabot", false)

	wantSection := ":package: dependabot bumped <u|PR #1: t>"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section = %q, want %q", s, wantSection)
	}
	if strings.Contains(got.Fallback, " ,") {
		t.Errorf("stranded comma in fallback %q", got.Fallback)
	}
}

func TestComposer_UpdatedMessage_Merged(t *testing.T) {
	c := slack.NewComposer("eyes")
	pr := slack.PRDetails{
		Repository: "octo/widget", Number: 7, Title: "feat", URL: "u", Author: "a", CreatedAt: created,
	}

	got := c.UpdatedMessage(pr, true, "twisted_rightwards_arrows")

	wantSection := ":twisted_rightwards_arrows: [Merged] ~<u|PR #7: feat>~"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section =\n  %q\nwant\n  %q", s, wantSection)
	}
	ctx, ok := contextText(t, got)
	if !ok || ctx != wantContext("octo/widget", "a") {
		t.Errorf("context = %q (present=%v), want %q", ctx, ok, wantContext("octo/widget", "a"))
	}
	if want := "[Merged] PR #7: feat by a"; got.Fallback != want {
		t.Errorf("fallback = %q, want %q", got.Fallback, want)
	}
}

func TestComposer_UpdatedMessage_Closed(t *testing.T) {
	c := slack.NewComposer("eyes")
	pr := slack.PRDetails{
		Repository: "octo/widget", Number: 7, Title: "feat", URL: "u", Author: "a", CreatedAt: created,
	}

	got := c.UpdatedMessage(pr, false, "x")

	wantSection := ":x: [Closed] ~<u|PR #7: feat>~"
	if s := sectionText(t, got); s != wantSection {
		t.Errorf("section =\n  %q\nwant\n  %q", s, wantSection)
	}
	if want := "[Closed] PR #7: feat by a"; got.Fallback != want {
		t.Errorf("fallback = %q, want %q", got.Fallback, want)
	}
}

func TestComposer_StuckDigestParent(t *testing.T) {
	c := slack.NewComposer("eyes")

	msg := c.StuckDigestParent([]string{"<!channel>"}, 2)
	got := sectionText(t, msg)

	for _, want := range []string{
		":hourglass_flowing_sand:",
		"<!channel>,",
		"2 open PRs waiting for review since before today:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("parent section missing %q\ngot: %s", want, got)
		}
	}
	if msg.Fallback != "2 PRs waiting for review" {
		t.Errorf("fallback = %q", msg.Fallback)
	}
}

func TestComposer_StuckDigestParent_SingularAndNoMentions(t *testing.T) {
	c := slack.NewComposer("eyes")

	msg := c.StuckDigestParent(nil, 1)
	got := sectionText(t, msg)

	if strings.Contains(got, ", ") {
		t.Errorf("empty mentions left a stranded separator: %s", got)
	}
	if !strings.Contains(got, "1 open PR waiting for review") {
		t.Errorf("singular headline wrong: %s", got)
	}
}

func TestComposer_StuckDigestList(t *testing.T) {
	c := slack.NewComposer("eyes")
	prs := []slack.StuckPR{
		{Repository: "octo/api", Number: 42, URL: "https://github.com/octo/api/pull/42", IdleDays: 1},
		{Repository: "octo/web", Number: 51, URL: "https://github.com/octo/web/pull/51", IdleDays: 3},
	}

	msg := c.StuckDigestList(prs)
	got := sectionText(t, msg)

	for _, want := range []string{
		"<https://github.com/octo/api/pull/42|octo/api #42> · idle 1 day",
		"<https://github.com/octo/web/pull/51|octo/web #51> · idle 3 days",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("list section missing %q\ngot: %s", want, got)
		}
	}
	// The list carries no headline — mentions and count live on the parent.
	if strings.Contains(got, "open PR") || strings.Contains(got, "hourglass") {
		t.Errorf("list should not repeat the parent headline: %s", got)
	}
}

// allSectionTexts returns the text of every section block, in order.
func allSectionTexts(m slack.Message) []string {
	var out []string
	for _, b := range m.Blocks {
		if b.Type == "section" && b.Text != nil {
			out = append(out, b.Text.Text)
		}
	}
	return out
}

func TestComposer_StuckDigestList_SplitsToRespectSlackSectionLimit(t *testing.T) {
	c := slack.NewComposer("eyes")

	// A busy channel: enough PRs that a single section would exceed Slack's
	// 3000-char section-text cap (which returns invalid_blocks).
	const n = 80
	prs := make([]slack.StuckPR, n)
	for i := range prs {
		prs[i] = slack.StuckPR{
			Repository: "mptooling/notifycat",
			Number:     1000 + i,
			URL:        fmt.Sprintf("https://github.com/mptooling/notifycat/pull/%d", 1000+i),
			IdleDays:   3,
		}
	}

	msg := c.StuckDigestList(prs)

	sections := allSectionTexts(msg)
	if len(sections) < 2 {
		t.Fatalf("expected the list to split into multiple sections; got %d", len(sections))
	}
	for i, s := range sections {
		if len(s) > 3000 {
			t.Errorf("section %d is %d chars; Slack rejects > 3000", i, len(s))
		}
	}
	// Every PR must still appear exactly once across the sections.
	joined := strings.Join(sections, "\n")
	for _, pr := range prs {
		needle := fmt.Sprintf("#%d>", pr.Number)
		if strings.Count(joined, needle) != 1 {
			t.Errorf("PR %d not rendered exactly once across sections", pr.Number)
		}
	}
	if len(msg.Blocks) > 50 {
		t.Errorf("message has %d blocks; Slack caps at 50", len(msg.Blocks))
	}
}
