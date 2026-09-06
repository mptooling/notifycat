package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/digest/domain"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/platform/slack"
)

func domainSectionText(message domain.Message) string {
	for _, block := range message.Blocks {
		if block.Type == "section" && block.Text != nil {
			return block.Text.Text
		}
	}
	return ""
}

func slackSectionText(message slack.Message) string {
	for _, block := range message.Blocks {
		if block.Type == "section" && block.Text != nil {
			return block.Text.Text
		}
	}
	return ""
}

// The SlackComposer adapter must preserve the underlying composer's rendered
// text and fallback when mapping slack.Message to the domain's neutral Message.
func TestSlackComposer_PreservesComposerOutput(t *testing.T) {
	composer := slack.NewComposer("eyes")
	adapter := NewSlackComposer(composer)
	domainPRs := []domain.StuckPR{{Repository: "acme/api", Number: 42, URL: "https://github.com/acme/api/pull/42", IdleDays: 2}}
	slackPRs := []slack.StuckPR{{Repository: "acme/api", Number: 42, URL: "https://github.com/acme/api/pull/42", IdleDays: 2}}

	gotParent := adapter.StuckDigestParent([]string{"<!channel>"}, 2)
	gotList := adapter.StuckDigestList(domainPRs)

	wantParent := composer.StuckDigestParent([]string{"<!channel>"}, 2)
	assert.Equal(t, slackSectionText(wantParent), domainSectionText(gotParent))
	assert.Equal(t, wantParent.Fallback, gotParent.Fallback)

	wantList := composer.StuckDigestList(slackPRs)
	assert.Equal(t, slackSectionText(wantList), domainSectionText(gotList))
	assert.Equal(t, wantList.Fallback, gotList.Fallback)
}

// The domain<->slack message mapping must round-trip block type, text, and
// fallback without loss (for the section blocks the digest emits).
func TestMessageMapping_RoundTrip(t *testing.T) {
	original := domain.Message{
		Blocks:   []domain.Block{{Type: "section", Text: &domain.TextObject{Type: "mrkdwn", Text: "hello"}}},
		Fallback: "fb",
	}

	slackMessage := toSlackMessage(original)

	require.Len(t, slackMessage.Blocks, 1)
	assert.Equal(t, "section", slackMessage.Blocks[0].Type)
	require.NotNil(t, slackMessage.Blocks[0].Text)
	assert.Equal(t, "mrkdwn", slackMessage.Blocks[0].Text.Type)
	assert.Equal(t, "hello", slackMessage.Blocks[0].Text.Text)
	assert.Equal(t, "fb", slackMessage.Fallback)
	assert.Equal(t, original, toDomainMessage(slackMessage))
}

// StuckRepo.FindStuck must map store rows (with preloaded messages) to digest
// domain PullRequests.
func TestStuckRepo_FindStuck_MapsRows(t *testing.T) {
	ctx := context.Background()
	db := persistence.NewTestDB(t)
	pullRequests := persistence.NewPullRequests(db)
	repo := NewStuckRepo(pullRequests)
	seededAt := time.Now().Add(-72 * time.Hour)
	require.NoError(t, persistence.RawCreateForTest(db, persistence.PullRequest{Repository: "acme/api", PRNumber: 42, UpdatedAt: seededAt}))
	require.NoError(t, pullRequests.AddMessage(ctx, "acme/api", 42, "C_ACME", "ts1"))
	cutoff := time.Now().Add(-24 * time.Hour)

	got, err := repo.FindStuck(ctx, cutoff)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "acme/api", got[0].Repository)
	assert.Equal(t, 42, got[0].PRNumber)
	assert.True(t, got[0].UpdatedAt.Before(cutoff))
}
