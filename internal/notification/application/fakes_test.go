package application_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/mptooling/notifycat/internal/notification/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// captureLogger returns a JSON logger writing into buf, for tests that assert an
// "ignored webhook event" reason field.
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, nil)), buf
}

// fakeMessenger records the domain intent each handler drives it with (no Slack
// types cross the port). Set the *Err fields to force a failure.
type openCall struct {
	channel string
	req     domain.OpenRequest
}
type closedCall struct {
	channel   string
	messageID string
	req       domain.ClosedRequest
}
type reviewFinishedCall struct {
	channel   string
	messageID string
	req       domain.ReviewFinishedRequest
}
type reactionCall struct {
	channel   string
	messageID string
	emoji     string
}
type deleteCall struct {
	channel   string
	messageID string
}

type fakeMessenger struct {
	opens          []openCall
	closes         []closedCall
	reviewFinished []reviewFinishedCall
	reactions      []reactionCall
	deletes        []deleteCall

	postErr   error
	updateErr error
	reactErr  error
	deleteErr error

	postedTS int
}

func (f *fakeMessenger) PostOpen(_ context.Context, channel string, req domain.OpenRequest) (string, error) {
	f.opens = append(f.opens, openCall{channel: channel, req: req})
	if f.postErr != nil {
		return "", f.postErr
	}
	f.postedTS++
	return fmt.Sprintf("ts-%d", f.postedTS), nil
}
func (f *fakeMessenger) UpdateClosed(_ context.Context, channel, messageID string, req domain.ClosedRequest) error {
	f.closes = append(f.closes, closedCall{channel: channel, messageID: messageID, req: req})
	return f.updateErr
}
func (f *fakeMessenger) UpdateReviewFinished(_ context.Context, channel, messageID string, req domain.ReviewFinishedRequest) error {
	f.reviewFinished = append(f.reviewFinished, reviewFinishedCall{channel: channel, messageID: messageID, req: req})
	return f.updateErr
}
func (f *fakeMessenger) AddReaction(_ context.Context, channel, messageID, emoji string) error {
	f.reactions = append(f.reactions, reactionCall{channel: channel, messageID: messageID, emoji: emoji})
	return f.reactErr
}
func (f *fakeMessenger) Delete(_ context.Context, channel, messageID string) error {
	f.deletes = append(f.deletes, deleteCall{channel: channel, messageID: messageID})
	return f.deleteErr
}

// reactionEmojis returns the emoji of every AddReaction call, in order.
func (f *fakeMessenger) reactionEmojis() []string {
	out := make([]string, len(f.reactions))
	for i, r := range f.reactions {
		out[i] = r.emoji
	}
	return out
}

// fakeMessageStore is an in-memory domain.MessageStore.
type fakeMessageStore struct {
	messages    map[string][]domain.Message
	touched     map[string]int
	closed      map[string]bool
	deleted     map[string]bool
	messagesErr error
}

func newFakeMessageStore() *fakeMessageStore {
	return &fakeMessageStore{
		messages: map[string][]domain.Message{},
		touched:  map[string]int{},
		closed:   map[string]bool{},
		deleted:  map[string]bool{},
	}
}

func storeKey(repository string, prNumber int) string {
	return fmt.Sprintf("%s#%d", repository, prNumber)
}

// seed pre-populates the stored messages for a PR.
func (f *fakeMessageStore) seed(repository string, prNumber int, messages ...domain.Message) {
	f.messages[storeKey(repository, prNumber)] = append(f.messages[storeKey(repository, prNumber)], messages...)
}

func (f *fakeMessageStore) AddMessage(_ context.Context, repository string, prNumber int, channel, messageID string) error {
	f.messages[storeKey(repository, prNumber)] = append(f.messages[storeKey(repository, prNumber)], domain.Message{Channel: channel, MessageID: messageID})
	return nil
}
func (f *fakeMessageStore) Messages(_ context.Context, repository string, prNumber int) ([]domain.Message, error) {
	if f.messagesErr != nil {
		return nil, f.messagesErr
	}
	msgs, ok := f.messages[storeKey(repository, prNumber)]
	if !ok {
		return nil, routingdomain.ErrNotFound
	}
	return msgs, nil
}
func (f *fakeMessageStore) Touch(_ context.Context, repository string, prNumber int) error {
	f.touched[storeKey(repository, prNumber)]++
	return nil
}
func (f *fakeMessageStore) MarkClosed(_ context.Context, repository string, prNumber int) error {
	f.closed[storeKey(repository, prNumber)] = true
	return nil
}
func (f *fakeMessageStore) Delete(_ context.Context, repository string, prNumber int) error {
	key := storeKey(repository, prNumber)
	f.deleted[key] = true
	delete(f.messages, key)
	return nil
}

// fakeTargetResolver is a domain.TargetResolver.
type fakeTargetResolver struct {
	behavior     routingdomain.RepoMapping
	targets      []routingdomain.Target
	changedFiles []string
	err          error
}

func (f *fakeTargetResolver) ResolveTargets(_ context.Context, _ string, _ int) (routingdomain.ResolvedTargets, error) {
	if f.err != nil {
		return routingdomain.ResolvedTargets{}, f.err
	}
	return routingdomain.ResolvedTargets{Mapping: f.behavior, Targets: f.targets, ChangedFiles: f.changedFiles}, nil
}

// fakeBehavior is a domain.RepoBehavior.
type fakeBehavior struct {
	mapping routingdomain.RepoMapping
	err     error
}

func (f *fakeBehavior) Get(_ context.Context, _ string) (routingdomain.RepoMapping, error) {
	return f.mapping, f.err
}

// fakeReviewSessions is a domain.ReviewSessions.
type fakeReviewSessions struct {
	active       domain.ReviewSession
	activeErr    error
	reviewers    []domain.ReviewSession
	reviewersErr error
	finished     int
}

func (f *fakeReviewSessions) GetActive(_ context.Context, _ string, _ int) (domain.ReviewSession, error) {
	return f.active, f.activeErr
}
func (f *fakeReviewSessions) Finish(_ context.Context, _ string, _ int) error {
	f.finished++
	return nil
}
func (f *fakeReviewSessions) Reviewers(_ context.Context, _ string, _ int) ([]domain.ReviewSession, error) {
	return f.reviewers, f.reviewersErr
}
