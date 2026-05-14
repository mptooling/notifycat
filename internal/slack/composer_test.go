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

	if !strings.HasPrefix(got, ":rocket: , please review ") {
		t.Fatalf("NewMessage with empty mentions = %q; want leading ':rocket: , please review '", got)
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
