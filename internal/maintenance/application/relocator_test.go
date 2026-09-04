package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/maintenance/application"
	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

type fakeTrackedLister struct {
	prs []domain.TrackedPR
	err error
}

func (f fakeTrackedLister) ListOpenWithMessages(context.Context) ([]domain.TrackedPR, error) {
	return f.prs, f.err
}

type moveCall struct {
	repository          string
	prNumber            int
	fromChannel, toRoom string
	messageID           string
}

type removeCall struct {
	repository string
	prNumber   int
	channel    string
}

type fakeRows struct {
	moves    []moveCall
	removals []removeCall
	moveErr  error
}

func (f *fakeRows) MoveMessage(_ context.Context, repository string, prNumber int, fromChannel, toChannel, messageID string) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	f.moves = append(f.moves, moveCall{repository: repository, prNumber: prNumber, fromChannel: fromChannel, toRoom: toChannel, messageID: messageID})
	return nil
}

func (f *fakeRows) RemoveMessage(_ context.Context, repository string, prNumber int, channel string) error {
	f.removals = append(f.removals, removeCall{repository: repository, prNumber: prNumber, channel: channel})
	return nil
}

type repostCall struct {
	from      domain.TrackedMessage
	toChannel string
}

type reactionCall struct {
	from, to domain.TrackedMessage
	allowed  []string
}

type fakeCourier struct {
	reposts      []repostCall
	reactions    []reactionCall
	deletions    []domain.TrackedMessage
	postedID     string
	repostErr    error
	reactionsErr error
}

func (f *fakeCourier) Repost(_ context.Context, from domain.TrackedMessage, toChannel string) (string, error) {
	f.reposts = append(f.reposts, repostCall{from: from, toChannel: toChannel})
	if f.repostErr != nil {
		return "", f.repostErr
	}
	return f.postedID, nil
}

func (f *fakeCourier) CopyReactions(_ context.Context, from, to domain.TrackedMessage, allowed []string) error {
	f.reactions = append(f.reactions, reactionCall{from: from, to: to, allowed: allowed})
	return f.reactionsErr
}

func (f *fakeCourier) Delete(_ context.Context, message domain.TrackedMessage) error {
	f.deletions = append(f.deletions, message)
	return nil
}

type fakeReactionPolicy struct{ emoji []string }

func (f fakeReactionPolicy) AllowedReactions(context.Context, string) ([]string, error) {
	return f.emoji, nil
}

type fakeChannelConfig struct{ byRepo map[string][]string }

func (f fakeChannelConfig) ConfiguredChannels(repository string) []string {
	return f.byRepo[repository]
}

func relocateLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// onePR is the common fixture: one open PR with a single message in C_OLD.
func onePR() fakeTrackedLister {
	return fakeTrackedLister{prs: []domain.TrackedPR{{
		Repository: "acme/api", PRNumber: 7,
		Messages: []domain.TrackedMessage{{Channel: "C_OLD", MessageID: "100.1"}},
	}}}
}

func newRelocator(params domain.RelocatorParams) *application.Relocator {
	if params.Logger == nil {
		params.Logger = relocateLogger()
	}
	if params.Reactions == nil {
		params.Reactions = fakeReactionPolicy{}
	}
	if params.Channels == nil {
		params.Channels = fakeChannelConfig{}
	}
	return application.NewRelocator(params)
}

func TestRelocator_Run_MovesMessageToDestination(t *testing.T) {
	rows := &fakeRows{}
	courier := &fakeCourier{postedID: "200.2"}
	relocator := newRelocator(domain.RelocatorParams{
		Lister:    onePR(),
		Rows:      rows,
		Courier:   courier,
		Reactions: fakeReactionPolicy{emoji: []string{"eyes", "white_check_mark"}},
		From:      "C_OLD",
		To:        "C_NEW",
	})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Moved: 1}, summary)

	require.Len(t, courier.reposts, 1)
	assert.Equal(t, repostCall{from: domain.TrackedMessage{Channel: "C_OLD", MessageID: "100.1"}, toChannel: "C_NEW"}, courier.reposts[0])
	assert.Equal(t, []moveCall{{repository: "acme/api", prNumber: 7, fromChannel: "C_OLD", toRoom: "C_NEW", messageID: "200.2"}}, rows.moves)
	require.Len(t, courier.reactions, 1)
	assert.Equal(t, []string{"eyes", "white_check_mark"}, courier.reactions[0].allowed)
	assert.Equal(t, domain.TrackedMessage{Channel: "C_NEW", MessageID: "200.2"}, courier.reactions[0].to)
	assert.Equal(t, []domain.TrackedMessage{{Channel: "C_OLD", MessageID: "100.1"}}, courier.deletions)
}

// A PR that already has a message in the destination (a fan-out overlap) must
// not get a second one — only the original is cleared away.
func TestRelocator_Run_MergesWhenDestinationAlreadyHasMessage(t *testing.T) {
	lister := fakeTrackedLister{prs: []domain.TrackedPR{{
		Repository: "acme/api", PRNumber: 7,
		Messages: []domain.TrackedMessage{
			{Channel: "C_OLD", MessageID: "100.1"},
			{Channel: "C_NEW", MessageID: "100.2"},
		},
	}}}
	rows := &fakeRows{}
	courier := &fakeCourier{postedID: "200.2"}
	relocator := newRelocator(domain.RelocatorParams{Lister: lister, Rows: rows, Courier: courier, From: "C_OLD", To: "C_NEW"})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Merged: 1}, summary)
	assert.Empty(t, courier.reposts, "the destination already has this PR")
	assert.Equal(t, []removeCall{{repository: "acme/api", prNumber: 7, channel: "C_OLD"}}, rows.removals)
	assert.Equal(t, []domain.TrackedMessage{{Channel: "C_OLD", MessageID: "100.1"}}, courier.deletions)
}

func TestRelocator_Run_DropsWhenNoDestinationGiven(t *testing.T) {
	rows := &fakeRows{}
	courier := &fakeCourier{}
	relocator := newRelocator(domain.RelocatorParams{Lister: onePR(), Rows: rows, Courier: courier, From: "C_OLD"})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Dropped: 1}, summary)
	assert.Empty(t, courier.reposts)
	assert.Equal(t, []removeCall{{repository: "acme/api", prNumber: 7, channel: "C_OLD"}}, rows.removals)
	assert.Equal(t, []domain.TrackedMessage{{Channel: "C_OLD", MessageID: "100.1"}}, courier.deletions)
}

func TestRelocator_Run_IgnoresPRsWithoutAMessageInSource(t *testing.T) {
	lister := fakeTrackedLister{prs: []domain.TrackedPR{{
		Repository: "acme/api", PRNumber: 7,
		Messages: []domain.TrackedMessage{{Channel: "C_SOMEWHERE", MessageID: "100.1"}},
	}}}
	rows := &fakeRows{}
	courier := &fakeCourier{postedID: "200.2"}
	relocator := newRelocator(domain.RelocatorParams{Lister: lister, Rows: rows, Courier: courier, From: "C_OLD", To: "C_NEW"})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Zero(t, summary)
	assert.Empty(t, courier.reposts)
	assert.Empty(t, rows.moves)
}

func TestRelocator_Run_RepositoryFilterNarrowsTheRun(t *testing.T) {
	lister := fakeTrackedLister{prs: []domain.TrackedPR{
		{Repository: "acme/api", PRNumber: 7, Messages: []domain.TrackedMessage{{Channel: "C_OLD", MessageID: "100.1"}}},
		{Repository: "acme/web", PRNumber: 8, Messages: []domain.TrackedMessage{{Channel: "C_OLD", MessageID: "100.2"}}},
	}}
	rows := &fakeRows{}
	relocator := newRelocator(domain.RelocatorParams{
		Lister: lister, Rows: rows, Courier: &fakeCourier{postedID: "200.2"},
		From: "C_OLD", To: "C_NEW", Repository: "acme/web",
	})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Moved: 1}, summary)
	require.Len(t, rows.moves, 1)
	assert.Equal(t, "acme/web", rows.moves[0].repository)
}

func TestRelocator_Run_DryRunWritesNothing(t *testing.T) {
	rows := &fakeRows{}
	courier := &fakeCourier{postedID: "200.2"}
	relocator := newRelocator(domain.RelocatorParams{
		Lister: onePR(), Rows: rows, Courier: courier,
		From: "C_OLD", To: "C_NEW", DryRun: true,
	})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Moved: 1}, summary)
	assert.Empty(t, courier.reposts)
	assert.Empty(t, courier.deletions)
	assert.Empty(t, rows.moves)
}

// The row is retargeted straight after the repost, so a failure there must
// leave the original message in place: a re-run then moves the PR again rather
// than leaving it with no message anywhere.
func TestRelocator_Run_KeepsOriginalWhenRetargetingFails(t *testing.T) {
	rows := &fakeRows{moveErr: errors.New("database is locked")}
	courier := &fakeCourier{postedID: "200.2"}
	relocator := newRelocator(domain.RelocatorParams{Lister: onePR(), Rows: rows, Courier: courier, From: "C_OLD", To: "C_NEW"})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Errors: 1}, summary)
	assert.Empty(t, courier.deletions, "the original survives a failed retarget")
}

// Reactions are decoration: failing to carry them over must not abandon a move
// that has already been recorded.
func TestRelocator_Run_ReactionFailureDoesNotFailTheMove(t *testing.T) {
	rows := &fakeRows{}
	courier := &fakeCourier{postedID: "200.2", reactionsErr: errors.New("missing_scope")}
	relocator := newRelocator(domain.RelocatorParams{Lister: onePR(), Rows: rows, Courier: courier, From: "C_OLD", To: "C_NEW"})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Moved: 1}, summary)
	assert.Len(t, courier.deletions, 1, "the original is still cleared away")
}

func TestRelocator_Run_RepostFailureLeavesEverythingAlone(t *testing.T) {
	rows := &fakeRows{}
	courier := &fakeCourier{repostErr: errors.New("unexpected message shape")}
	relocator := newRelocator(domain.RelocatorParams{Lister: onePR(), Rows: rows, Courier: courier, From: "C_OLD", To: "C_NEW"})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Errors: 1}, summary)
	assert.Empty(t, rows.moves)
	assert.Empty(t, courier.deletions)
}

// A message the messenger no longer has cannot be carried anywhere, so the row
// is dropped rather than counted as a failure that a re-run would repeat.
func TestRelocator_Run_MissingOriginalDropsTheRow(t *testing.T) {
	rows := &fakeRows{}
	courier := &fakeCourier{repostErr: domain.ErrMessageGone}
	relocator := newRelocator(domain.RelocatorParams{Lister: onePR(), Rows: rows, Courier: courier, From: "C_OLD", To: "C_NEW"})

	summary, err := relocator.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, domain.RelocateSummary{Scanned: 1, Dropped: 1}, summary)
	assert.Equal(t, []removeCall{{repository: "acme/api", prNumber: 7, channel: "C_OLD"}}, rows.removals)
	assert.Empty(t, courier.deletions, "there is nothing left to delete")
}

func TestRelocator_Audit_ReportsRowsInUnconfiguredChannels(t *testing.T) {
	lister := fakeTrackedLister{prs: []domain.TrackedPR{
		{Repository: "acme/api", PRNumber: 7, Messages: []domain.TrackedMessage{
			{Channel: "C_OLD", MessageID: "100.1"},
			{Channel: "C_KEEP", MessageID: "100.2"},
		}},
		{Repository: "acme/web", PRNumber: 8, Messages: []domain.TrackedMessage{
			{Channel: "C_WEB", MessageID: "200.1"},
		}},
	}}
	channels := fakeChannelConfig{byRepo: map[string][]string{
		"acme/api": {"C_KEEP"},
		"acme/web": {"C_WEB"},
	}}
	relocator := newRelocator(domain.RelocatorParams{Lister: lister, Rows: &fakeRows{}, Courier: &fakeCourier{}, Channels: channels})

	stale, err := relocator.Audit(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []domain.StaleMessage{{Repository: "acme/api", PRNumber: 7, Channel: "C_OLD"}}, stale)
}
