package infrastructure

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/slack"
)

func testMessenger(t *testing.T) (*SlackMessenger, *slack.Composer) {
	t.Helper()

	composer := slack.NewComposer("eyes")
	client := slack.NewClient(http.DefaultClient, "xoxb-test")
	return NewSlackMessenger(client, composer), composer
}

func samplePR() kernel.PR {
	return kernel.PR{Number: 42, Title: "Fix", URL: "https://github.com/acme/api/pull/42", Author: "alice"}
}

func TestSlackMessenger_ComposeOpen_StandardVsBot(t *testing.T) {
	messenger, composer := testMessenger(t)
	pullRequest := samplePR()
	details := prDetails("acme/api", pullRequest)

	standard := messenger.composeOpen(domain.OpenRequest{
		Repository: "acme/api", PR: pullRequest, Mentions: []string{"<!channel>"}, NewPREmoji: "eyes",
	})
	bot := messenger.composeOpen(domain.OpenRequest{
		Repository: "acme/api", PR: pullRequest, Bot: &domain.BotFormat{Name: "dependabot", Security: true},
	})

	assert.Equal(t, composer.NewMessage(details, []string{"<!channel>"}, "eyes"), standard)
	assert.Equal(t, composer.BotMessage(details, nil, "dependabot", true), bot, "a bot request takes the compact template")
}

func TestSlackMessenger_ComposeClosed_AppendsReviewedBy(t *testing.T) {
	messenger, composer := testMessenger(t)
	pullRequest := samplePR()
	details := prDetails("acme/api", pullRequest)
	base := composer.UpdatedMessage(details, true, "tada")

	noReviewers := messenger.composeClosed(domain.ClosedRequest{
		Repository: "acme/api", PR: pullRequest, Merged: true, Emoji: "tada",
	})
	withReviewers := messenger.composeClosed(domain.ClosedRequest{
		Repository: "acme/api", PR: pullRequest, Merged: true, Emoji: "tada", ReviewerIDs: []string{"U1", "U2"},
	})

	assert.Equal(t, base, noReviewers)
	require.Len(t, withReviewers.Blocks, len(base.Blocks)+1)
	assert.Equal(t, base.Blocks, withReviewers.Blocks[:len(base.Blocks)])
	assert.Equal(t, composer.ReviewedByMarker([]string{"U1", "U2"}), withReviewers.Blocks[len(base.Blocks)])
}

func TestSlackMessenger_ComposeReviewFinished_RebuildsWithReviewers(t *testing.T) {
	messenger, composer := testMessenger(t)
	pullRequest := samplePR()
	details := prDetails("acme/api", pullRequest)
	base := composer.NewMessage(details, nil, "eyes")

	got := messenger.composeReviewFinished(domain.ReviewFinishedRequest{
		Repository: "acme/api", PR: pullRequest, NewPREmoji: "eyes", ReviewerIDs: []string{"U1"},
	})

	require.Len(t, got.Blocks, len(base.Blocks)+1)
	assert.Equal(t, base.Blocks, got.Blocks[:len(base.Blocks)], "the message is rebuilt from the standard template")
	assert.Equal(t, composer.ReviewedByMarker([]string{"U1"}), got.Blocks[len(base.Blocks)])
}
