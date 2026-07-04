package infrastructure

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

func domainSectionText(m domain.Message) string {
	for _, b := range m.Blocks {
		if b.Type == "section" && b.Text != nil {
			return b.Text.Text
		}
	}
	return ""
}

func slackSectionText(m slack.Message) string {
	for _, b := range m.Blocks {
		if b.Type == "section" && b.Text != nil {
			return b.Text.Text
		}
	}
	return ""
}

// The SlackComposer adapter must preserve the underlying composer's rendered
// text and fallback when mapping slack.Message to the domain's neutral Message.
func TestSlackComposer_PreservesComposerOutput(t *testing.T) {
	raw := slack.NewComposer("eyes")
	adapter := NewSlackComposer(raw)

	wantParent := raw.StuckDigestParent([]string{"<!channel>"}, 2)
	gotParent := adapter.StuckDigestParent([]string{"<!channel>"}, 2)
	if domainSectionText(gotParent) != slackSectionText(wantParent) {
		t.Errorf("parent section text = %q; want %q", domainSectionText(gotParent), slackSectionText(wantParent))
	}
	if gotParent.Fallback != wantParent.Fallback {
		t.Errorf("parent fallback = %q; want %q", gotParent.Fallback, wantParent.Fallback)
	}

	domainPRs := []domain.StuckPR{{Repository: "acme/api", Number: 42, URL: "https://github.com/acme/api/pull/42", IdleDays: 2}}
	slackPRs := []slack.StuckPR{{Repository: "acme/api", Number: 42, URL: "https://github.com/acme/api/pull/42", IdleDays: 2}}
	wantList := raw.StuckDigestList(slackPRs)
	gotList := adapter.StuckDigestList(domainPRs)
	if domainSectionText(gotList) != slackSectionText(wantList) {
		t.Errorf("list section text = %q; want %q", domainSectionText(gotList), slackSectionText(wantList))
	}
	if gotList.Fallback != wantList.Fallback {
		t.Errorf("list fallback = %q; want %q", gotList.Fallback, wantList.Fallback)
	}
}

// The domain<->slack message mapping must round-trip block type, text, and
// fallback without loss (for the section blocks the digest emits).
func TestMessageMapping_RoundTrip(t *testing.T) {
	original := domain.Message{
		Blocks:   []domain.Block{{Type: "section", Text: &domain.TextObject{Type: "mrkdwn", Text: "hello"}}},
		Fallback: "fb",
	}
	slackMsg := toSlackMessage(original)
	if len(slackMsg.Blocks) != 1 || slackMsg.Blocks[0].Type != "section" ||
		slackMsg.Blocks[0].Text == nil || slackMsg.Blocks[0].Text.Text != "hello" || slackMsg.Blocks[0].Text.Type != "mrkdwn" {
		t.Fatalf("toSlackMessage lost block data: %+v", slackMsg)
	}
	if slackMsg.Fallback != "fb" {
		t.Errorf("fallback = %q; want fb", slackMsg.Fallback)
	}
	if back := toDomainMessage(slackMsg); !reflect.DeepEqual(back, original) {
		t.Errorf("round-trip = %+v; want %+v", back, original)
	}
}

// StuckRepo.FindStuck must map store rows (with preloaded messages) to digest
// domain PullRequests.
func TestStuckRepo_FindStuck_MapsRows(t *testing.T) {
	db := store.NewTestDB(t)
	pullRequests := store.NewPullRequests(db)
	repo := NewStuckRepo(pullRequests)

	old := time.Now().Add(-72 * time.Hour)
	if err := store.RawCreateForTest(db, store.PullRequest{Repository: "acme/api", PRNumber: 42, UpdatedAt: old}); err != nil {
		t.Fatalf("seed pr: %v", err)
	}
	if err := pullRequests.AddMessage(context.Background(), "acme/api", 42, "C_ACME", "ts1"); err != nil {
		t.Fatalf("add message: %v", err)
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	got, err := repo.FindStuck(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("FindStuck: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("FindStuck returned %d rows; want 1", len(got))
	}
	pr := got[0]
	if pr.Repository != "acme/api" || pr.PRNumber != 42 {
		t.Errorf("row = %+v; want acme/api #42", pr)
	}
	if !pr.UpdatedAt.Before(cutoff) {
		t.Errorf("UpdatedAt = %v; want before cutoff %v", pr.UpdatedAt, cutoff)
	}
	want := []domain.MessageRef{{Channel: "C_ACME", MessageID: "ts1"}}
	if !reflect.DeepEqual(pr.Messages, want) {
		t.Errorf("messages = %+v; want %+v", pr.Messages, want)
	}
}
