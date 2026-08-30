package slack_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/platform/slack"
)

// created is a fixed PR creation time used across composer tests. Its localized
// fallback ("Jun 5, 2026 at 2:04 PM") is what Slack shows when a client cannot
// render the date token.
var created = time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC)

func samplePR() slack.PRDetails {
	return slack.PRDetails{
		Repository: "octo/widget",
		Number:     42,
		Title:      "fix the thing",
		URL:        "https://github.com/octo/widget/pull/42",
		Author:     "alice",
		CreatedAt:  created,
	}
}

func sectionText(t *testing.T, message slack.Message) string {
	t.Helper()

	sections := sectionTexts(message)
	require.NotEmpty(t, sections, "no section block in %+v", message)
	return sections[0]
}

// sectionTexts returns the text of every section block, in order.
func sectionTexts(message slack.Message) []string {
	var texts []string
	for _, block := range message.Blocks {
		if block.Type == "section" && block.Text != nil {
			texts = append(texts, block.Text.Text)
		}
	}
	return texts
}

// contextTexts returns the first element text of every context block, in order.
func contextTexts(message slack.Message) []string {
	var texts []string
	for _, block := range message.Blocks {
		if block.Type == "context" && len(block.Elements) > 0 {
			texts = append(texts, block.Elements[0].Text)
		}
	}
	return texts
}

func contextText(t *testing.T, message slack.Message) string {
	t.Helper()

	contexts := contextTexts(message)
	require.NotEmpty(t, contexts, "no context block in %+v", message)
	return contexts[0]
}

// actionButtons returns the buttons of the first actions block.
func actionButtons(message slack.Message) []slack.Button {
	for _, block := range message.Blocks {
		if block.Type == "actions" {
			return block.Buttons
		}
	}
	return nil
}

// wantContext builds the expected context line for a PR opened at `created`.
func wantContext(repository, author string) string {
	return fmt.Sprintf("%s · %s · opened <!date^%d^{date_short_pretty} at {time}|Jun 5, 2026 at 2:04 PM>",
		repository, author, created.Unix())
}

func TestComposer_NewMessage(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.NewMessage(samplePR(), []string{"@bob", "@carol"}, "rocket")

	assert.Equal(t, ":rocket: @bob,@carol, please review <https://github.com/octo/widget/pull/42|PR #42: fix the thing>", sectionText(t, got))
	assert.Equal(t, wantContext("octo/widget", "alice"), contextText(t, got))
	assert.Equal(t, "@bob,@carol, please review PR #42: fix the thing by alice", got.Fallback)
}

func TestComposer_NewMessage_NoMentions(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a", CreatedAt: created,
	}, nil, "rocket")

	assert.Equal(t, ":rocket: please review <u|PR #1: t>", sectionText(t, got))
	assert.NotContains(t, got.Fallback, " ,", "no stranded comma without mentions")
	assert.False(t, strings.HasPrefix(got.Fallback, ", "))
}

func TestComposer_NewMessage_ChannelFallback(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a", CreatedAt: created,
	}, []string{"<!channel>"}, "rocket")

	assert.Equal(t, ":rocket: <!channel>, please review <u|PR #1: t>", sectionText(t, got))
}

func TestComposer_NewMessage_NoCreatedAt(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a",
	}, nil, "rocket")

	assert.Equal(t, "octo/widget · a", contextText(t, got), "a zero created time drops the 'opened' clause")
}

func TestComposer_NewMessage_HasStartReviewButton(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.NewMessage(samplePR(), []string{"@bob"}, "rocket")

	buttons := actionButtons(got)
	require.Len(t, buttons, 1)
	assert.Equal(t, "start_review", buttons[0].ActionID)
	assert.Equal(t, "octo/widget#42", buttons[0].Value)
	assert.Equal(t, "primary", buttons[0].Style)
	assert.Equal(t, "Start review", buttons[0].Text)
	assert.Equal(t, "https://github.com/octo/widget/pull/42", buttons[0].URL)
}

func TestComposer_BotMessage_Routine(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.BotMessage(slack.PRDetails{
		Number: 42,
		Title:  "bump acme/lib from 1.2.0 to 1.2.1",
		URL:    "https://github.com/octo/widget/pull/42",
	}, []string{"@bob"}, "dependabot", false)

	assert.Equal(t, ":package: @bob, dependabot bumped <https://github.com/octo/widget/pull/42|PR #42: bump acme/lib from 1.2.0 to 1.2.1>", sectionText(t, got))
	assert.Empty(t, contextTexts(got), "a bot message stays compact")
	assert.NotContains(t, got.Fallback, "please review")
}

func TestComposer_BotMessage_Security(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.BotMessage(slack.PRDetails{
		Number: 42,
		Title:  "bump acme/lib from 1.2.0 to 1.2.1",
		URL:    "https://github.com/octo/widget/pull/42",
	}, []string{"@bob", "@carol"}, "renovate", true)

	assert.Equal(t, ":rotating_light: @bob,@carol, renovate security update <https://github.com/octo/widget/pull/42|PR #42: bump acme/lib from 1.2.0 to 1.2.1>", sectionText(t, got))
}

func TestComposer_BotMessage_NoMentions(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.BotMessage(slack.PRDetails{Number: 1, Title: "t", URL: "u"}, nil, "dependabot", false)

	assert.Equal(t, ":package: dependabot bumped <u|PR #1: t>", sectionText(t, got))
	assert.NotContains(t, got.Fallback, " ,")
}

func TestComposer_BotMessage_NoButton(t *testing.T) {
	composer := slack.NewComposer("eyes")

	got := composer.BotMessage(slack.PRDetails{Number: 1, Title: "t", URL: "u"}, nil, "dependabot", false)

	assert.Empty(t, actionButtons(got), "the compact bot message carries no button")
}

func TestComposer_UpdatedMessage_Merged(t *testing.T) {
	composer := slack.NewComposer("eyes")
	pullRequest := slack.PRDetails{
		Repository: "octo/widget", Number: 7, Title: "feat", URL: "u", Author: "a", CreatedAt: created,
	}

	got := composer.UpdatedMessage(pullRequest, true, "twisted_rightwards_arrows")

	assert.Equal(t, ":twisted_rightwards_arrows: [Merged] ~<u|PR #7: feat>~", sectionText(t, got))
	assert.Equal(t, wantContext("octo/widget", "a"), contextText(t, got))
	assert.Equal(t, "[Merged] PR #7: feat by a", got.Fallback)
}

func TestComposer_UpdatedMessage_Closed(t *testing.T) {
	composer := slack.NewComposer("eyes")
	pullRequest := slack.PRDetails{
		Repository: "octo/widget", Number: 7, Title: "feat", URL: "u", Author: "a", CreatedAt: created,
	}

	got := composer.UpdatedMessage(pullRequest, false, "x")

	assert.Equal(t, ":x: [Closed] ~<u|PR #7: feat>~", sectionText(t, got))
	assert.Equal(t, "[Closed] PR #7: feat by a", got.Fallback)
}

func TestComposer_StuckDigestParent(t *testing.T) {
	composer := slack.NewComposer("eyes")

	message := composer.StuckDigestParent([]string{"<!channel>"}, 2)

	got := sectionText(t, message)
	assert.Contains(t, got, ":hourglass_flowing_sand:")
	assert.Contains(t, got, "<!channel>,")
	assert.Contains(t, got, "2 open PRs waiting for review since before today:")
	assert.Equal(t, "2 PRs waiting for review", message.Fallback)
}

func TestComposer_StuckDigestParent_SingularAndNoMentions(t *testing.T) {
	composer := slack.NewComposer("eyes")

	message := composer.StuckDigestParent(nil, 1)

	got := sectionText(t, message)
	assert.NotContains(t, got, ", ", "empty mentions leave no stranded separator")
	assert.Contains(t, got, "1 open PR waiting for review")
}

func TestComposer_StuckDigestList(t *testing.T) {
	composer := slack.NewComposer("eyes")
	stuck := []slack.StuckPR{
		{Repository: "octo/api", Number: 42, URL: "https://github.com/octo/api/pull/42", IdleDays: 1},
		{Repository: "octo/web", Number: 51, URL: "https://github.com/octo/web/pull/51", IdleDays: 3},
	}

	message := composer.StuckDigestList(stuck)

	got := sectionText(t, message)
	assert.Contains(t, got, "<https://github.com/octo/api/pull/42|octo/api #42> · idle 1 day")
	assert.Contains(t, got, "<https://github.com/octo/web/pull/51|octo/web #51> · idle 3 days")
	assert.NotContains(t, got, "open PR", "mentions and count live on the parent")
	assert.NotContains(t, got, "hourglass")
}

func TestComposer_StuckDigestList_SplitsToRespectSlackSectionLimit(t *testing.T) {
	composer := slack.NewComposer("eyes")
	// A busy channel: enough PRs that a single section would exceed Slack's
	// 3000-char section-text cap (which returns invalid_blocks).
	stuck := make([]slack.StuckPR, 80)
	for i := range stuck {
		stuck[i] = slack.StuckPR{
			Repository: "mptooling/notifycat",
			Number:     1000 + i,
			URL:        fmt.Sprintf("https://github.com/mptooling/notifycat/pull/%d", 1000+i),
			IdleDays:   3,
		}
	}

	message := composer.StuckDigestList(stuck)

	sections := sectionTexts(message)
	require.Greater(t, len(sections), 1, "the list must split across sections")
	for i, section := range sections {
		assert.LessOrEqual(t, len(section), 3000, "section %d exceeds Slack's cap", i)
	}
	joined := strings.Join(sections, "\n")
	for _, pullRequest := range stuck {
		assert.Equal(t, 1, strings.Count(joined, fmt.Sprintf("#%d>", pullRequest.Number)),
			"PR %d must be rendered exactly once", pullRequest.Number)
	}
	assert.LessOrEqual(t, len(message.Blocks), 50, "Slack caps a message at 50 blocks")
}

func TestComposer_ActionsBlockMarshalsToButtonJSON(t *testing.T) {
	composer := slack.NewComposer("eyes")
	got := composer.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 42, Title: "t", URL: "u", Author: "a", CreatedAt: created,
	}, nil, "rocket")

	var actions slack.Block
	for _, block := range got.Blocks {
		if block.Type == "actions" {
			actions = block
		}
	}
	raw, err := json.Marshal(actions)

	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"actions","elements":[{"type":"button","text":{"type":"plain_text","text":"Start review"},"action_id":"start_review","value":"octo/widget#42","style":"primary","url":"u"}]}`, string(raw))
}

// The block-model extension must leave section/context JSON byte-for-byte as the
// original struct-tag marshaling produced it, so existing Slack rendering and
// any wire-format expectations hold. This compares each block against a
// reference struct carrying the pre-change tags.
func TestComposer_SectionAndContextJSONUnchanged(t *testing.T) {
	type plainBlock struct {
		Type     string             `json:"type"`
		Text     *slack.TextObject  `json:"text,omitempty"`
		Elements []slack.TextObject `json:"elements,omitempty"`
	}

	composer := slack.NewComposer("eyes")
	got := composer.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a", CreatedAt: created,
	}, nil, "rocket")

	for _, block := range got.Blocks {
		if block.Type == "actions" {
			continue
		}
		t.Run(block.Type, func(t *testing.T) {
			gotJSON, err := json.Marshal(block)
			require.NoError(t, err)
			wantJSON, err := json.Marshal(plainBlock{Type: block.Type, Text: block.Text, Elements: block.Elements})
			require.NoError(t, err)

			assert.JSONEq(t, string(wantJSON), string(gotJSON))
		})
	}
}

func TestComposer_ReviewingMarker(t *testing.T) {
	composer := slack.NewComposer("eyes")
	since := time.Date(2026, 6, 5, 14, 4, 0, 0, time.UTC)

	block := composer.ReviewingMarker("U123", since)

	assert.Equal(t, "context", block.Type)
	require.Len(t, block.Elements, 1)
	for _, want := range []string{":eye:", "<@U123>", "reviewing", "since", "Jun 5, 2026 at 2:04 PM"} {
		assert.Contains(t, block.Elements[0].Text, want)
	}
}

func TestComposer_ReviewedByMarker(t *testing.T) {
	composer := slack.NewComposer("eyes")

	block := composer.ReviewedByMarker([]string{"U1", "U2"})

	assert.Equal(t, "context", block.Type)
	require.Len(t, block.Elements, 1)
	assert.Equal(t, "reviewed by <@U1>, <@U2>", block.Elements[0].Text)
}
