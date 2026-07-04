package infrastructure

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/slack"
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

// composeOpen must select the standard template by default and the compact bot
// template when Bot is set — matching a direct composer call each time.
func TestSlackMessenger_ComposeOpen_StandardVsBot(t *testing.T) {
	m, composer := testMessenger(t)
	pr := samplePR()
	details := prDetails("acme/api", pr)

	standard := m.composeOpen(domain.OpenRequest{Repository: "acme/api", PR: pr, Mentions: []string{"<!channel>"}, NewPREmoji: "eyes"})
	if want := composer.NewMessage(details, []string{"<!channel>"}, "eyes"); !reflect.DeepEqual(standard, want) {
		t.Errorf("standard open != composer.NewMessage\ngot:  %+v\nwant: %+v", standard, want)
	}

	bot := m.composeOpen(domain.OpenRequest{Repository: "acme/api", PR: pr, Bot: &domain.BotFormat{Name: "dependabot", Security: true}})
	if want := composer.BotMessage(details, nil, "dependabot", true); !reflect.DeepEqual(bot, want) {
		t.Errorf("bot open != composer.BotMessage\ngot:  %+v\nwant: %+v", bot, want)
	}
}

// composeClosed must equal UpdatedMessage without reviewers, and append exactly
// one ReviewedBy marker block when reviewers are present.
func TestSlackMessenger_ComposeClosed_AppendsReviewedBy(t *testing.T) {
	m, composer := testMessenger(t)
	pr := samplePR()
	details := prDetails("acme/api", pr)

	noReviewers := m.composeClosed(domain.ClosedRequest{Repository: "acme/api", PR: pr, Merged: true, Emoji: "tada"})
	if want := composer.UpdatedMessage(details, true, "tada"); !reflect.DeepEqual(noReviewers, want) {
		t.Errorf("closed without reviewers != composer.UpdatedMessage")
	}

	withReviewers := m.composeClosed(domain.ClosedRequest{Repository: "acme/api", PR: pr, Merged: true, Emoji: "tada", ReviewerIDs: []string{"U1", "U2"}})
	base := composer.UpdatedMessage(details, true, "tada")
	if len(withReviewers.Blocks) != len(base.Blocks)+1 {
		t.Fatalf("closed with reviewers has %d blocks; want %d", len(withReviewers.Blocks), len(base.Blocks)+1)
	}
	if want := composer.ReviewedByMarker([]string{"U1", "U2"}); !reflect.DeepEqual(withReviewers.Blocks[len(withReviewers.Blocks)-1], want) {
		t.Errorf("last block is not the ReviewedBy marker")
	}
	if !reflect.DeepEqual(withReviewers.Blocks[:len(base.Blocks)], base.Blocks) {
		t.Errorf("closed base blocks != UpdatedMessage blocks")
	}
}

// composeReviewFinished must rebuild a fresh standard message and append a
// ReviewedBy marker when reviewers exist.
func TestSlackMessenger_ComposeReviewFinished_RebuildsWithReviewers(t *testing.T) {
	m, composer := testMessenger(t)
	pr := samplePR()
	details := prDetails("acme/api", pr)

	got := m.composeReviewFinished(domain.ReviewFinishedRequest{Repository: "acme/api", PR: pr, NewPREmoji: "eyes", ReviewerIDs: []string{"U1"}})
	base := composer.NewMessage(details, nil, "eyes")
	if len(got.Blocks) != len(base.Blocks)+1 {
		t.Fatalf("review-finished has %d blocks; want %d", len(got.Blocks), len(base.Blocks)+1)
	}
	if !reflect.DeepEqual(got.Blocks[:len(base.Blocks)], base.Blocks) {
		t.Errorf("rebuilt base != fresh NewMessage")
	}
	if want := composer.ReviewedByMarker([]string{"U1"}); !reflect.DeepEqual(got.Blocks[len(got.Blocks)-1], want) {
		t.Errorf("last block is not the ReviewedBy marker")
	}
}
