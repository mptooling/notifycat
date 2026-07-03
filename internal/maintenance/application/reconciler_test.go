package application_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/maintenance/application"
	"github.com/mptooling/notifycat/internal/maintenance/domain"
)

type fakeLister struct{ rows []domain.PRRow }

func (f fakeLister) ListOpen(context.Context) ([]domain.PRRow, error) { return f.rows, nil }

// fakeChecker reports openness by (repo,pr) key; missing key returns an error.
type fakeChecker struct {
	open map[string]bool
	err  map[string]error
}

func key(repo string, pr int) string { return repo + "#" + itoa(pr) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (f fakeChecker) IsOpen(_ context.Context, repo string, pr int) (bool, error) {
	if err, ok := f.err[key(repo, pr)]; ok {
		return false, err
	}
	return f.open[key(repo, pr)], nil
}

// fakeStore records MarkClosed and Delete calls so tests can assert which path
// each row took. It satisfies both domain.Closer and domain.Deleter.
type fakeStore struct {
	closed  []string
	deleted []string
}

func (f *fakeStore) MarkClosed(_ context.Context, repo string, pr int) error {
	f.closed = append(f.closed, key(repo, pr))
	return nil
}

func (f *fakeStore) Delete(_ context.Context, repo string, pr int) error {
	f.deleted = append(f.deleted, key(repo, pr))
	return nil
}

func newReconciler(lister domain.OpenLister, checker domain.PRChecker, closer domain.Closer, deleter domain.Deleter, logger *slog.Logger, dryRun bool) *application.Reconciler {
	return application.NewReconciler(domain.ReconcilerParams{
		Lister:  lister,
		Checker: checker,
		Closer:  closer,
		Deleter: deleter,
		Logger:  logger,
		DryRun:  dryRun,
	})
}

func rows() []domain.PRRow {
	return []domain.PRRow{
		{PRNumber: 1, Repository: "o/r"}, // open
		{PRNumber: 2, Repository: "o/r"}, // closed → mark
		{PRNumber: 3, Repository: "o/r"}, // errors → skip
	}
}

func TestReconciler_MarksClosedOnly(t *testing.T) {
	checker := fakeChecker{
		open: map[string]bool{key("o/r", 1): true, key("o/r", 2): false},
		err:  map[string]error{key("o/r", 3): errors.New("boom")},
	}
	closer := &fakeStore{}
	r := newReconciler(fakeLister{rows()}, checker, closer, closer, discardLogger(), false)

	s, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(closer.closed) != 1 || closer.closed[0] != key("o/r", 2) {
		t.Fatalf("closed = %v; want only o/r#2", closer.closed)
	}
	if s.Checked != 3 || s.Closed != 1 || s.StillOpen != 1 || s.Errors != 1 {
		t.Fatalf("summary = %+v; want checked=3 closed=1 open=1 errors=1", s)
	}
}

func TestReconciler_NotFoundIsRemovedNotErrored(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 9): domain.ErrPRNotFound}}
	closer := &fakeStore{}
	prs := []domain.PRRow{{PRNumber: 9, Repository: "o/r"}}
	r := newReconciler(fakeLister{prs}, checker, closer, closer, discardLogger(), false)

	s, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(closer.closed) != 1 || closer.closed[0] != key("o/r", 9) {
		t.Fatalf("closed = %v; want o/r#9 removed from the digest", closer.closed)
	}
	if s.Checked != 1 || s.Removed != 1 || s.Errors != 0 || s.Closed != 0 {
		t.Fatalf("summary = %+v; want checked=1 removed=1 errors=0 closed=0", s)
	}
}

func TestReconciler_DraftIsRemovedNotErrored(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 7): domain.ErrPRDraft}}
	closer := &fakeStore{}
	prs := []domain.PRRow{{PRNumber: 7, Repository: "o/r"}}
	r := newReconciler(fakeLister{prs}, checker, closer, closer, discardLogger(), false)

	s, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(closer.deleted) != 1 || closer.deleted[0] != key("o/r", 7) {
		t.Fatalf("deleted = %v; want o/r#7 deleted from the db", closer.deleted)
	}
	if len(closer.closed) != 0 {
		t.Fatalf("closed = %v; a draft must be deleted, not marked closed", closer.closed)
	}
	if s.Checked != 1 || s.Removed != 1 || s.Errors != 0 || s.Closed != 0 {
		t.Fatalf("summary = %+v; want checked=1 removed=1 errors=0 closed=0", s)
	}
}

func TestReconciler_DryRunDoesNotDeleteDraft(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 7): domain.ErrPRDraft}}
	closer := &fakeStore{}
	prs := []domain.PRRow{{PRNumber: 7, Repository: "o/r"}}
	r := newReconciler(fakeLister{prs}, checker, closer, closer, discardLogger(), true)

	s, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(closer.deleted) != 0 {
		t.Fatalf("dry-run deleted %v; want nothing", closer.deleted)
	}
	if s.Removed != 1 {
		t.Fatalf("summary = %+v; want removed=1 (would)", s)
	}
}

func TestReconciler_DryRunDoesNotRemoveNotFound(t *testing.T) {
	checker := fakeChecker{err: map[string]error{key("o/r", 9): domain.ErrPRNotFound}}
	closer := &fakeStore{}
	prs := []domain.PRRow{{PRNumber: 9, Repository: "o/r"}}
	r := newReconciler(fakeLister{prs}, checker, closer, closer, discardLogger(), true)

	s, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(closer.closed) != 0 {
		t.Fatalf("dry-run wrote %v; want nothing", closer.closed)
	}
	if s.Removed != 1 {
		t.Fatalf("summary = %+v; want removed=1 (would)", s)
	}
}

func TestReconciler_DryRunWritesNothing(t *testing.T) {
	checker := fakeChecker{open: map[string]bool{key("o/r", 1): true, key("o/r", 2): false, key("o/r", 3): false}}
	closer := &fakeStore{}
	r := newReconciler(fakeLister{rows()}, checker, closer, closer, discardLogger(), true)

	s, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(closer.closed) != 0 {
		t.Fatalf("dry-run wrote %v; want nothing", closer.closed)
	}
	if s.Closed != 2 || s.StillOpen != 1 {
		t.Fatalf("summary = %+v; want closed=2 (would), open=1", s)
	}
}
