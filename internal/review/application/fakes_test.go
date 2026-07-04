package application_test

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/mptooling/notifycat/internal/review/domain"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type startCall struct {
	repository string
	prNumber   int
	userID     string
	userName   string
}

type fakeRecorder struct {
	active    bool
	activeErr error
	startErr  error
	started   []startCall
}

func (f *fakeRecorder) HasActiveReview(_ context.Context, _ string, _ int, _ string) (bool, error) {
	return f.active, f.activeErr
}
func (f *fakeRecorder) Start(_ context.Context, repository string, prNumber int, userID, userName string) error {
	f.started = append(f.started, startCall{repository: repository, prNumber: prNumber, userID: userID, userName: userName})
	return f.startErr
}

type fakeMessageChecker struct {
	has bool
	err error
}

func (f *fakeMessageChecker) HasMessages(_ context.Context, _ string, _ int) (bool, error) {
	return f.has, f.err
}

type decorateCall struct {
	message  domain.MessageRef
	reviewer domain.Reviewer
	since    time.Time
}

type fakeDecorator struct {
	calls []decorateCall
	err   error
}

func (f *fakeDecorator) AppendReviewingMarker(_ context.Context, message domain.MessageRef, reviewer domain.Reviewer, since time.Time) error {
	f.calls = append(f.calls, decorateCall{message: message, reviewer: reviewer, since: since})
	return f.err
}
