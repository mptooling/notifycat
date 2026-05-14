package pullrequest_test

import (
	"context"
	"errors"
	"slices"

	"github.com/mptooling/notifycat/internal/store"
)

// fakeSlackMessages is an in-memory implementation of the SlackMessages
// dependency. Keyed by (repository, pr_number) like the production schema.
type fakeSlackMessages struct {
	rows map[fakeKey]store.SlackMessage
}

type fakeKey struct {
	repo string
	pr   int
}

func newFakeSlackMessages() *fakeSlackMessages {
	return &fakeSlackMessages{rows: map[fakeKey]store.SlackMessage{}}
}

func (f *fakeSlackMessages) Save(_ context.Context, m store.SlackMessage) error {
	f.rows[fakeKey{m.Repository, m.PRNumber}] = m
	return nil
}

func (f *fakeSlackMessages) Get(_ context.Context, repository string, prNumber int) (store.SlackMessage, error) {
	m, ok := f.rows[fakeKey{repository, prNumber}]
	if !ok {
		return store.SlackMessage{}, store.ErrNotFound
	}
	return m, nil
}

func (f *fakeSlackMessages) Delete(_ context.Context, repository string, prNumber int) error {
	delete(f.rows, fakeKey{repository, prNumber})
	return nil
}

// fakeRepoMappings is an in-memory implementation of the RepoMappings
// dependency.
type fakeRepoMappings struct {
	byRepo map[string]store.RepoMapping
}

func newFakeRepoMappings(initial ...store.RepoMapping) *fakeRepoMappings {
	f := &fakeRepoMappings{byRepo: map[string]store.RepoMapping{}}
	for _, m := range initial {
		f.byRepo[m.Repository] = m
	}
	return f
}

func (f *fakeRepoMappings) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	m, ok := f.byRepo[repository]
	if !ok {
		return store.RepoMapping{}, store.ErrNotFound
	}
	return m, nil
}

// fakeSlackClient records every call so tests can assert what happened.
type fakeSlackClient struct {
	postedTSCounter int
	calls           []slackCall

	// Test seams: failure injection.
	postErr   error
	updateErr error
	deleteErr error
	reactErr  error
}

type slackCall struct {
	Method  string
	Channel string
	TS      string
	Text    string
	Name    string
}

func (f *fakeSlackClient) PostMessage(_ context.Context, channel, text string) (string, error) {
	if f.postErr != nil {
		return "", f.postErr
	}
	f.postedTSCounter++
	ts := tsForCounter(f.postedTSCounter)
	f.calls = append(f.calls, slackCall{Method: "PostMessage", Channel: channel, Text: text, TS: ts})
	return ts, nil
}

func (f *fakeSlackClient) UpdateMessage(_ context.Context, channel, ts, text string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.calls = append(f.calls, slackCall{Method: "UpdateMessage", Channel: channel, TS: ts, Text: text})
	return nil
}

func (f *fakeSlackClient) DeleteMessage(_ context.Context, channel, ts string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.calls = append(f.calls, slackCall{Method: "DeleteMessage", Channel: channel, TS: ts})
	return nil
}

func (f *fakeSlackClient) AddReaction(_ context.Context, channel, ts, name string) error {
	if f.reactErr != nil {
		return f.reactErr
	}
	f.calls = append(f.calls, slackCall{Method: "AddReaction", Channel: channel, TS: ts, Name: name})
	return nil
}

func (f *fakeSlackClient) methods() []string {
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Method
	}
	return out
}

func tsForCounter(n int) string {
	// Deterministic, unique-ish ts values for tests.
	return "1700000000.000" + string(rune('0'+n))
}

// errInjected is a sentinel used by tests that inject failures.
var errInjected = errors.New("injected failure")

// containsMethod returns true if methods contains m.
func containsMethod(methods []string, m string) bool { return slices.Contains(methods, m) }
