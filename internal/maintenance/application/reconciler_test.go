package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/maintenance/application"
	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

type fakeLister struct{ rows []domain.PRRow }

func (f fakeLister) ListOpen(context.Context) ([]domain.PRRow, error) { return f.rows, nil }

// fakeChecker reports openness by (repo, pr) key; a key in err fails instead.
type fakeChecker struct {
	open map[string]bool
	err  map[string]error
}

func key(repository string, prNumber int) string {
	return repository + "#" + strconv.Itoa(prNumber)
}

func (f fakeChecker) IsOpen(_ context.Context, repository string, prNumber int) (bool, error) {
	if err, ok := f.err[key(repository, prNumber)]; ok {
		return false, err
	}
	return f.open[key(repository, prNumber)], nil
}

// fakeStore records MarkClosed and Delete calls so tests can assert which path
// each row took. It satisfies both domain.Closer and domain.Deleter.
type fakeStore struct {
	closed  []string
	deleted []string
}

func (f *fakeStore) MarkClosed(_ context.Context, repository string, prNumber int) error {
	f.closed = append(f.closed, key(repository, prNumber))
	return nil
}

func (f *fakeStore) Delete(_ context.Context, repository string, prNumber int) error {
	f.deleted = append(f.deleted, key(repository, prNumber))
	return nil
}

func newReconciler(lister domain.OpenLister, checker domain.PRChecker, store *fakeStore, logger *slog.Logger, dryRun bool) *application.Reconciler {
	return application.NewReconciler(domain.ReconcilerParams{
		Lister:  lister,
		Checker: checker,
		Closer:  store,
		Deleter: store,
		Logger:  logger,
		DryRun:  dryRun,
	})
}

// singleRow is the one-PR listing most reconciler cases run against.
func singleRow(prNumber int) fakeLister {
	return fakeLister{rows: []domain.PRRow{{PRNumber: prNumber, Repository: "o/r"}}}
}

func threeRows() fakeLister {
	return fakeLister{rows: []domain.PRRow{
		{PRNumber: 1, Repository: "o/r"},
		{PRNumber: 2, Repository: "o/r"},
		{PRNumber: 3, Repository: "o/r"},
	}}
}

func TestReconciler_BitbucketProviderLogsBitbucketURL(t *testing.T) {
	var logged bytes.Buffer
	store := &fakeStore{}
	reconciler := application.NewReconciler(domain.ReconcilerParams{
		Lister:   singleRow(7),
		Checker:  fakeChecker{err: map[string]error{key("o/r", 7): domain.ErrPRDraft}},
		Closer:   store,
		Deleter:  store,
		Logger:   slog.New(slog.NewJSONHandler(&logged, nil)),
		Provider: kernel.ProviderBitbucket,
	})

	_, err := reconciler.Run(context.Background())

	require.NoError(t, err)
	assert.Contains(t, logged.String(), "https://bitbucket.org/o/r/pull-requests/7")
}

func TestReconciler_MarksClosedOnly(t *testing.T) {
	checker := fakeChecker{
		open: map[string]bool{key("o/r", 1): true, key("o/r", 2): false},
		err:  map[string]error{key("o/r", 3): errors.New("boom")},
	}
	store := &fakeStore{}
	reconciler := newReconciler(threeRows(), checker, store, discardLogger(), false)

	summary, err := reconciler.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{key("o/r", 2)}, store.closed)
	assert.Equal(t, 3, summary.Checked)
	assert.Equal(t, 1, summary.Closed)
	assert.Equal(t, 1, summary.StillOpen)
	assert.Equal(t, 1, summary.Errors)
}

func TestReconciler_NotFoundIsRemovedNotErrored(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 9): domain.ErrPRNotFound}}
	store := &fakeStore{}
	reconciler := newReconciler(singleRow(9), checker, store, discardLogger(), false)

	summary, err := reconciler.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{key("o/r", 9)}, store.closed, "a vanished PR leaves the digest")
	assert.Equal(t, 1, summary.Checked)
	assert.Equal(t, 1, summary.Removed)
	assert.Zero(t, summary.Errors)
	assert.Zero(t, summary.Closed)
}

func TestReconciler_DraftIsRemovedNotErrored(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 7): domain.ErrPRDraft}}
	store := &fakeStore{}
	reconciler := newReconciler(singleRow(7), checker, store, discardLogger(), false)

	summary, err := reconciler.Run(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{key("o/r", 7)}, store.deleted)
	assert.Empty(t, store.closed, "a draft is deleted, not marked closed")
	assert.Equal(t, 1, summary.Checked)
	assert.Equal(t, 1, summary.Removed)
	assert.Zero(t, summary.Errors)
	assert.Zero(t, summary.Closed)
}

func TestReconciler_DryRunDoesNotDeleteDraft(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 7): domain.ErrPRDraft}}
	store := &fakeStore{}
	reconciler := newReconciler(singleRow(7), checker, store, discardLogger(), true)

	summary, err := reconciler.Run(context.Background())

	require.NoError(t, err)
	assert.Empty(t, store.deleted)
	assert.Equal(t, 1, summary.Removed, "the summary still reports what it would have removed")
}

func TestReconciler_DryRunDoesNotRemoveNotFound(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 9): domain.ErrPRNotFound}}
	store := &fakeStore{}
	reconciler := newReconciler(singleRow(9), checker, store, discardLogger(), true)

	summary, err := reconciler.Run(context.Background())

	require.NoError(t, err)
	assert.Empty(t, store.closed)
	assert.Equal(t, 1, summary.Removed)
}

func TestReconciler_DryRunWritesNothing(t *testing.T) {
	checker := fakeChecker{open: map[string]bool{key("o/r", 1): true, key("o/r", 2): false, key("o/r", 3): false}}
	store := &fakeStore{}
	reconciler := newReconciler(threeRows(), checker, store, discardLogger(), true)

	summary, err := reconciler.Run(context.Background())

	require.NoError(t, err)
	assert.Empty(t, store.closed)
	assert.Equal(t, 2, summary.Closed, "the summary reports what it would have closed")
	assert.Equal(t, 1, summary.StillOpen)
}
