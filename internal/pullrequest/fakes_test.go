package pullrequest_test

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/store"
)

// fakeSlackMessages is an in-memory implementation of the SlackMessages
// dependency. Keyed by (repository, pr_number) like the production schema.
type fakeSlackMessages struct {
	rows    map[fakeKey]store.SlackMessage
	touched []fakeKey
	closed  []fakeKey
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

func (f *fakeSlackMessages) Touch(_ context.Context, repository string, prNumber int) error {
	f.touched = append(f.touched, fakeKey{repository, prNumber})
	return nil
}

func (f *fakeSlackMessages) MarkClosed(_ context.Context, repository string, prNumber int) error {
	f.closed = append(f.closed, fakeKey{repository, prNumber})
	return nil
}

// fakeRepoMappings is an in-memory Resolver: the handlers route through
// Resolve, which here ignores the PR number and returns the stored mapping by
// repository (path resolution itself is covered by router_test.go).
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

func (f *fakeRepoMappings) Resolve(_ context.Context, repository string, _ int) (store.RepoMapping, error) {
	m, ok := f.byRepo[repository]
	if !ok {
		return store.RepoMapping{}, store.ErrNotFound
	}
	return m, nil
}

// fakeMessenger is a fake Messenger implementation that records every call so tests can assert what happened.
type fakeMessenger struct {
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
	// Msg is the composed Block Kit message for PostMessage/UpdateMessage calls.
	Msg slack.Message
	// Text is the rendered headline (section block) text, kept as a convenience
	// for substring assertions about the visible message.
	Text string
	Name string
}

func (f *fakeMessenger) PostMessage(_ context.Context, channel string, msg slack.Message) (string, error) {
	if f.postErr != nil {
		return "", f.postErr
	}
	f.postedTSCounter++
	ts := tsForCounter(f.postedTSCounter)
	f.calls = append(f.calls, slackCall{Method: "PostMessage", Channel: channel, Msg: msg, Text: sectionTextOf(msg), TS: ts})
	return ts, nil
}

func (f *fakeMessenger) UpdateMessage(_ context.Context, channel, messageID string, msg slack.Message) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.calls = append(f.calls, slackCall{Method: "UpdateMessage", Channel: channel, TS: messageID, Msg: msg, Text: sectionTextOf(msg)})
	return nil
}

// sectionTextOf returns the first section block's text, or "" if absent.
func sectionTextOf(m slack.Message) string {
	for _, b := range m.Blocks {
		if b.Type == "section" && b.Text != nil {
			return b.Text.Text
		}
	}
	return ""
}

// contextTextOf returns the first context block's text, or "" if absent.
func contextTextOf(m slack.Message) string {
	for _, b := range m.Blocks {
		if b.Type == "context" && len(b.Elements) > 0 {
			return b.Elements[0].Text
		}
	}
	return ""
}

func (f *fakeMessenger) DeleteMessage(_ context.Context, channel, messageID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.calls = append(f.calls, slackCall{Method: "DeleteMessage", Channel: channel, TS: messageID})
	return nil
}

func (f *fakeMessenger) AddReaction(_ context.Context, channel, messageID, name string) error {
	if f.reactErr != nil {
		return f.reactErr
	}
	f.calls = append(f.calls, slackCall{Method: "AddReaction", Channel: channel, TS: messageID, Name: name})
	return nil
}

func (f *fakeMessenger) methods() []string {
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

// fakePRStore is an in-memory PullRequestStore. Messages are keyed by
// "repo#prNumber" and deduped by channel within each PR.
type fakePRStore struct {
	rows    map[string][]store.Message
	touched map[string]int
	closed  map[string]int
	deleted map[string]int
}

func newFakePRStore() *fakePRStore {
	return &fakePRStore{
		rows:    map[string][]store.Message{},
		touched: map[string]int{},
		closed:  map[string]int{},
		deleted: map[string]int{},
	}
}

func prStoreKey(repository string, prNumber int) string {
	return fmt.Sprintf("%s#%d", repository, prNumber)
}

func (f *fakePRStore) AddMessage(_ context.Context, repository string, prNumber int, channel, messageID string) error {
	key := prStoreKey(repository, prNumber)
	for _, m := range f.rows[key] {
		if m.Channel == channel {
			return nil // dedupe by channel
		}
	}
	f.rows[key] = append(f.rows[key], store.Message{Channel: channel, MessageID: messageID})
	return nil
}

func (f *fakePRStore) Messages(_ context.Context, repository string, prNumber int) ([]store.Message, error) {
	key := prStoreKey(repository, prNumber)
	msgs, ok := f.rows[key]
	if !ok {
		return nil, store.ErrNotFound
	}
	return msgs, nil
}

func (f *fakePRStore) Touch(_ context.Context, repository string, prNumber int) error {
	f.touched[prStoreKey(repository, prNumber)]++
	return nil
}

func (f *fakePRStore) MarkClosed(_ context.Context, repository string, prNumber int) error {
	f.closed[prStoreKey(repository, prNumber)]++
	return nil
}

func (f *fakePRStore) Delete(_ context.Context, repository string, prNumber int) error {
	f.deleted[prStoreKey(repository, prNumber)]++
	delete(f.rows, prStoreKey(repository, prNumber))
	return nil
}

// touched returns the touch call count for a given PR.
func (f *fakePRStore) touchedCount(repository string, prNumber int) int {
	return f.touched[prStoreKey(repository, prNumber)]
}

// fakeTargetResolver is an in-memory TargetResolver that returns fixed
// behavior+targets (or a fixed error).
type fakeTargetResolver struct {
	behavior store.RepoMapping
	targets  []store.Target
	err      error
}

func (f *fakeTargetResolver) ResolveTargets(_ context.Context, _ string, _ int) (store.RepoMapping, []store.Target, error) {
	if f.err != nil {
		return store.RepoMapping{}, nil, f.err
	}
	return f.behavior, f.targets, nil
}

// postsByChannel counts PostMessage calls per channel on fakeMessenger.
func (f *fakeMessenger) postsByChannel() map[string]int {
	out := map[string]int{}
	for _, c := range f.calls {
		if c.Method == "PostMessage" {
			out[c.Channel]++
		}
	}
	return out
}

// updates returns the total number of UpdateMessage calls recorded.
func (f *fakeMessenger) updates() int {
	n := 0
	for _, c := range f.calls {
		if c.Method == "UpdateMessage" {
			n++
		}
	}
	return n
}

// reactions returns the total number of AddReaction calls recorded.
func (f *fakeMessenger) reactions() int {
	n := 0
	for _, c := range f.calls {
		if c.Method == "AddReaction" {
			n++
		}
	}
	return n
}

// fakeBehavior is an in-memory RepoBehavior that returns a fixed RepoMapping
// (or a fixed error). Use err = store.ErrNotFound to simulate no mapping.
type fakeBehavior struct {
	m   store.RepoMapping
	err error
}

func (f *fakeBehavior) Get(_ context.Context, _ string) (store.RepoMapping, error) {
	if f.err != nil {
		return store.RepoMapping{}, f.err
	}
	return f.m, nil
}
