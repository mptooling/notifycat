package slack_test

import (
	"strings"
	"testing"

	"github.com/mptooling/notifycat/internal/slack"
)

func TestComposer_NewMessage(t *testing.T) {
	c := slack.NewComposer("large_green_circle")

	got := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget",
		Number:     42,
		Title:      "fix the thing",
		URL:        "https://github.com/octo/widget/pull/42",
		Author:     "alice",
	}, []string{"@bob", "@carol"})

	want := ":large_green_circle: @bob,@carol, please review <https://github.com/octo/widget/pull/42|PR #42: fix the thing> by alice"
	if got != want {
		t.Fatalf("NewMessage =\n  %q\nwant\n  %q", got, want)
	}
}

func TestComposer_NewMessage_NoMentions(t *testing.T) {
	c := slack.NewComposer("rocket")

	got := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a",
	}, nil)

	want := ":rocket: please review <u|PR #1: t> by a"
	if got != want {
		t.Fatalf("NewMessage with empty mentions =\n  %q\nwant\n  %q", got, want)
	}
	if strings.Contains(got, ": ,") || strings.Contains(got, " ,") {
		t.Errorf("stranded comma in %q", got)
	}
}

func TestComposer_NewMessage_ChannelFallback(t *testing.T) {
	c := slack.NewComposer("rocket")

	got := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 1, Title: "t", URL: "u", Author: "a",
	}, []string{"<!channel>"})

	want := ":rocket: <!channel>, please review <u|PR #1: t> by a"
	if got != want {
		t.Fatalf("NewMessage with @channel fallback =\n  %q\nwant\n  %q", got, want)
	}
}

func TestComposer_BotMessage_Routine(t *testing.T) {
	c := slack.NewComposer("large_green_circle")

	got := c.BotMessage(slack.PRDetails{
		Number: 42,
		Title:  "bump acme/lib from 1.2.0 to 1.2.1",
		URL:    "https://github.com/octo/widget/pull/42",
	}, []string{"@bob"}, "dependabot", false)

	want := ":package: @bob, dependabot bumped <https://github.com/octo/widget/pull/42|PR #42: bump acme/lib from 1.2.0 to 1.2.1>"
	if got != want {
		t.Fatalf("BotMessage(routine) =\n  %q\nwant\n  %q", got, want)
	}
	if strings.Contains(got, "please review") {
		t.Errorf("routine bot message should not say 'please review': %q", got)
	}
}

func TestComposer_BotMessage_Security(t *testing.T) {
	c := slack.NewComposer("rocket")

	got := c.BotMessage(slack.PRDetails{
		Number: 42,
		Title:  "bump acme/lib from 1.2.0 to 1.2.1",
		URL:    "https://github.com/octo/widget/pull/42",
	}, []string{"@bob", "@carol"}, "renovate", true)

	want := ":rotating_light: @bob,@carol, renovate security update <https://github.com/octo/widget/pull/42|PR #42: bump acme/lib from 1.2.0 to 1.2.1>"
	if got != want {
		t.Fatalf("BotMessage(security) =\n  %q\nwant\n  %q", got, want)
	}
}

func TestComposer_BotMessage_NoMentions(t *testing.T) {
	c := slack.NewComposer("rocket")

	got := c.BotMessage(slack.PRDetails{
		Number: 1, Title: "t", URL: "u",
	}, nil, "dependabot", false)

	want := ":package: dependabot bumped <u|PR #1: t>"
	if got != want {
		t.Fatalf("BotMessage with empty mentions =\n  %q\nwant\n  %q", got, want)
	}
	if strings.Contains(got, ", ,") || strings.Contains(got, " ,") {
		t.Errorf("stranded comma in %q", got)
	}
}

func TestComposer_UpdatedMessage_Merged(t *testing.T) {
	c := slack.NewComposer("rocket")
	original := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 7, Title: "feat", URL: "u", Author: "a",
	}, []string{"@b"})

	got := c.UpdatedMessage(true, original)
	want := "[Merged] ~" + original + "~"
	if got != want {
		t.Fatalf("UpdatedMessage(merged) =\n  %q\nwant\n  %q", got, want)
	}
}

func TestComposer_UpdatedMessage_Closed(t *testing.T) {
	c := slack.NewComposer("rocket")
	original := c.NewMessage(slack.PRDetails{
		Repository: "octo/widget", Number: 7, Title: "feat", URL: "u", Author: "a",
	}, []string{"@b"})

	got := c.UpdatedMessage(false, original)
	want := "[Closed] ~" + original + "~"
	if got != want {
		t.Fatalf("UpdatedMessage(closed) =\n  %q\nwant\n  %q", got, want)
	}
}
