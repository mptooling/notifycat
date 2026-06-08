package reconcile_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/mptooling/notifycat/internal/reconcile"
	"github.com/mptooling/notifycat/internal/store"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeLister struct{ rows []store.SlackMessage }

func (f fakeLister) ListOpen(context.Context) ([]store.SlackMessage, error) { return f.rows, nil }

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

type fakeCloser struct{ closed []string }

func (f *fakeCloser) MarkClosed(_ context.Context, repo string, pr int) error {
	f.closed = append(f.closed, key(repo, pr))
	return nil
}

func rows() []store.SlackMessage {
	return []store.SlackMessage{
		{PRNumber: 1, Repository: "o/r", TS: "t1"}, // open
		{PRNumber: 2, Repository: "o/r", TS: "t2"}, // closed → mark
		{PRNumber: 3, Repository: "o/r", TS: "t3"}, // errors → skip
	}
}

func TestReconciler_MarksClosedOnly(t *testing.T) {
	checker := fakeChecker{
		open: map[string]bool{key("o/r", 1): true, key("o/r", 2): false},
		err:  map[string]error{key("o/r", 3): errors.New("boom")},
	}
	closer := &fakeCloser{}
	r := reconcile.NewReconciler(fakeLister{rows()}, checker, closer, discardLogger(), false)

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

func TestReconciler_DryRunWritesNothing(t *testing.T) {
	checker := fakeChecker{open: map[string]bool{key("o/r", 1): true, key("o/r", 2): false, key("o/r", 3): false}}
	closer := &fakeCloser{}
	r := reconcile.NewReconciler(fakeLister{rows()}, checker, closer, discardLogger(), true)

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
