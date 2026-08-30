package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

const (
	testSecret  = "topsecret"
	testRepo    = "octo/widget"
	testChannel = "C0123ABCDE"
	testTS      = "1717171717.000100"
)

var testReactions = diagnosticsdomain.SmokeReactionsConfig{
	Enabled:       true,
	NewPR:         "large_green_circle",
	MergedPR:      "twisted_rightwards_arrows",
	Approved:      "white_check_mark",
	Commented:     "speech_balloon",
	RequestChange: "exclamation",
}

// fakeMappings answers Get for exactly one repository.
type fakeMappings struct {
	repo    string
	channel string
}

func (f fakeMappings) Get(_ context.Context, repository string) (routingdomain.RepoMapping, error) {
	if repository != f.repo {
		return routingdomain.RepoMapping{}, routingdomain.ErrNotFound
	}
	return routingdomain.RepoMapping{Repository: repository, SlackChannel: f.channel}, nil
}

// fakeMessages returns a fixed message or ErrNotFound when ts is empty.
type fakeMessages struct {
	ts        string
	gotRepo   string
	gotNumber int
}

func (f *fakeMessages) Messages(_ context.Context, repository string, prNumber int) ([]diagnosticsdomain.SmokeMessage, error) {
	f.gotRepo = repository
	f.gotNumber = prNumber
	if f.ts == "" {
		return nil, routingdomain.ErrNotFound
	}
	return []diagnosticsdomain.SmokeMessage{{Channel: "C0SMOKE", MessageID: f.ts}}, nil
}

// fakeCleanup records Delete calls.
type fakeCleanup struct {
	deleteErr     error
	deleteCalled  bool
	deletedRepo   string
	deletedNumber int
}

func (f *fakeCleanup) DeletePR(_ context.Context, repository string, prNumber int) error {
	f.deleteCalled = true
	f.deletedRepo = repository
	f.deletedNumber = prNumber
	return f.deleteErr
}

// fakeReactions stands in for the Slack reactions read.
type fakeReactions struct {
	names []string
	err   error
	calls int
}

func (f *fakeReactions) Reactions(_ context.Context, _, _ string) ([]string, error) {
	f.calls++
	return f.names, f.err
}

// fakeSender records each POST, returns a configured status, and optionally
// returns a transport error.
type fakeSender struct {
	mu           sync.Mutex
	sends        []fakeSend
	statusCode   int // returned for every Send (default 200)
	transportErr error
}

type fakeSend struct {
	url     string
	body    []byte
	headers map[string]string
}

func (f *fakeSender) Send(_ context.Context, url string, body []byte, headers map[string]string) (int, error) {
	if f.transportErr != nil {
		return 0, f.transportErr
	}
	f.mu.Lock()
	f.sends = append(f.sends, fakeSend{url: url, body: append([]byte(nil), body...), headers: copyHeaders(headers)})
	f.mu.Unlock()
	if f.statusCode == 0 {
		return 200, nil
	}
	return f.statusCode, nil
}

func (f *fakeSender) captured() []fakeSend {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeSend(nil), f.sends...)
}

func copyHeaders(headers map[string]string) map[string]string {
	copied := make(map[string]string, len(headers))
	for name, value := range headers {
		copied[name] = value
	}
	return copied
}

// fakeSigner records what it was asked to sign, returning a deterministic header.
type fakeSigner struct {
	mu     sync.Mutex
	signed [][]byte
	secret string
	header string
	value  string
}

func (f *fakeSigner) Sign(secret string, body []byte) (header, value string) {
	f.mu.Lock()
	f.signed = append(f.signed, append([]byte(nil), body...))
	f.secret = secret
	f.mu.Unlock()
	if f.header == "" {
		return "X-Hub-Signature-256", "sha256=fakesig"
	}
	return f.header, f.value
}

// fakeWebhookBuilder reproduces only the GitHub-shaped fields these tests decode, so the
// application test stays independent of the infrastructure layer. The real
// GitHubWebhookBuilder's full wire format is covered by github_webhook_builder_test.go.
type fakeWebhookBuilder struct{}

func (fakeWebhookBuilder) Build(_ string, number int, title string, event diagnosticsdomain.SmokeEvent) (diagnosticsdomain.ForgedWebhook, error) {
	type review struct {
		State string `json:"state"`
	}
	payload := struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Merged bool   `json:"merged"`
		} `json:"pull_request"`
		Review *review `json:"review,omitempty"`
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}{}
	payload.PullRequest.Number = number
	payload.PullRequest.Title = title
	payload.Sender.Type = "User"

	eventValue := "pull_request"
	switch event.Kind {
	case diagnosticsdomain.SmokeOpened:
		payload.Action = "opened"
	case diagnosticsdomain.SmokeCommented:
		eventValue = "pull_request_review"
		payload.Action = "submitted"
		payload.Review = &review{State: "commented"}
		if event.IsBot {
			payload.Sender.Type = "Bot"
		}
	case diagnosticsdomain.SmokeApproved:
		eventValue = "pull_request_review"
		payload.Action = "submitted"
		payload.Review = &review{State: "approved"}
	case diagnosticsdomain.SmokeMerged:
		payload.Action = "closed"
		payload.PullRequest.Merged = true
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return diagnosticsdomain.ForgedWebhook{}, err
	}
	return diagnosticsdomain.ForgedWebhook{EventHeader: "X-GitHub-Event", EventValue: eventValue, Body: body}, nil
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1717171717, 0) }
}

func defaultConfig() diagnosticsdomain.SmokeConfig {
	return diagnosticsdomain.SmokeConfig{
		WebhookURL:      "http://fake",
		WebhookSecret:   testSecret,
		IgnoreAIReviews: false,
		Reactions:       testReactions,
		Now:             fixedClock(),
	}
}

func newSmoke(
	sender *fakeSender,
	messages *fakeMessages,
	reactions *fakeReactions,
	cleanup *fakeCleanup,
	cfg diagnosticsdomain.SmokeConfig,
) *application.SmokeUseCase {
	return application.NewSmokeUseCase(
		fakeMappings{repo: testRepo, channel: testChannel},
		messages,
		reactions,
		cleanup,
		&fakeSigner{},
		fakeWebhookBuilder{},
		sender,
		cfg,
	)
}

// deliveredEvent is the routing-relevant view of one captured delivery.
type deliveredEvent struct {
	EventValue  string
	Action      string
	ReviewState string
	Merged      bool
	Number      int
	Title       string
	SenderType  string
}

func decodeSend(t *testing.T, send fakeSend) deliveredEvent {
	t.Helper()

	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Merged bool   `json:"merged"`
		} `json:"pull_request"`
		Review *struct {
			State string `json:"state"`
		} `json:"review"`
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}
	require.NoError(t, json.Unmarshal(send.body, &payload), "body = %s", send.body)

	delivered := deliveredEvent{
		EventValue: send.headers["X-GitHub-Event"],
		Action:     payload.Action,
		Merged:     payload.PullRequest.Merged,
		Number:     payload.PullRequest.Number,
		Title:      payload.PullRequest.Title,
		SenderType: payload.Sender.Type,
	}
	if payload.Review != nil {
		delivered.ReviewState = payload.Review.State
	}
	return delivered
}

func decodeSends(t *testing.T, sends []fakeSend) []deliveredEvent {
	t.Helper()

	decoded := make([]deliveredEvent, len(sends))
	for i, send := range sends {
		decoded[i] = decodeSend(t, send)
	}
	return decoded
}

// reactionSteps lists the lifecycle steps recorded in the result.
func reactionSteps(result diagnosticsdomain.SmokeResult) []string {
	steps := make([]string, len(result.Reactions))
	for i, check := range result.Reactions {
		steps[i] = check.Step
	}
	return steps
}

func TestSmokeRun_OpenedDelivery_DrivesEndpointAndReportsChannelAndTS(t *testing.T) {
	sender := &fakeSender{}
	messages := &fakeMessages{ts: testTS}
	reactions := &fakeReactions{}

	result, err := newSmoke(sender, messages, reactions, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, false)

	require.NoError(t, err)
	sends := sender.captured()
	require.Len(t, sends, 1, "without --reactions only the open event is replayed")
	delivered := decodeSend(t, sends[0])
	assert.Equal(t, "pull_request", delivered.EventValue)
	assert.Equal(t, "opened", delivered.Action)
	assert.Contains(t, delivered.Title, "[notifycat smoke]")
	assert.NotEmpty(t, sends[0].headers["X-Hub-Signature-256"])
	assert.Zero(t, reactions.calls)
	assert.Equal(t, testChannel, result.Channel)
	assert.Equal(t, testTS, result.Timestamp)
	assert.Equal(t, delivered.Number, messages.gotNumber)
	assert.Empty(t, result.Reactions)
}

func TestSmokeRun_WithReactions_RunsLifecycleAndVerifiesEachEmoji(t *testing.T) {
	sender := &fakeSender{}
	reactions := &fakeReactions{names: []string{testReactions.Commented, testReactions.Approved, testReactions.MergedPR}}

	result, err := newSmoke(sender, &fakeMessages{ts: testTS}, reactions, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err)
	delivered := decodeSends(t, sender.captured())
	require.Len(t, delivered, 4, "opened, comment, approve, merge")
	assert.Equal(t, "pull_request", delivered[0].EventValue)
	assert.Equal(t, "opened", delivered[0].Action)
	assert.Equal(t, "pull_request_review", delivered[1].EventValue)
	assert.Equal(t, "submitted", delivered[1].Action)
	assert.Equal(t, "commented", delivered[1].ReviewState)
	assert.Equal(t, "pull_request_review", delivered[2].EventValue)
	assert.Equal(t, "approved", delivered[2].ReviewState)
	assert.Equal(t, "pull_request", delivered[3].EventValue)
	assert.Equal(t, "closed", delivered[3].Action)
	assert.True(t, delivered[3].Merged)

	for i, event := range delivered {
		assert.Equal(t, delivered[0].Number, event.Number, "send %d must reuse the same PR number", i)
	}

	require.Len(t, result.Reactions, 3)
	assert.Equal(t, []string{"comment", "approve", "merge"}, reactionSteps(result))
	assert.Equal(t, []string{testReactions.Commented, testReactions.Approved, testReactions.MergedPR},
		[]string{result.Reactions[0].Emoji, result.Reactions[1].Emoji, result.Reactions[2].Emoji})
	for _, check := range result.Reactions {
		assert.True(t, check.Present, "step %s", check.Step)
		assert.NoError(t, check.VerifyErr, "step %s", check.Step)
	}
}

func TestSmokeRun_WithReactions_BotMarkerConfigured_ReplaysBotReviewAndVerifiesMarker(t *testing.T) {
	sender := &fakeSender{}
	cfg := defaultConfig()
	cfg.Reactions.BotReview = "robot_face"
	reactions := &fakeReactions{names: []string{
		cfg.Reactions.Commented, cfg.Reactions.BotReview, cfg.Reactions.Approved, cfg.Reactions.MergedPR,
	}}

	result, err := newSmoke(sender, &fakeMessages{ts: testTS}, reactions, &fakeCleanup{}, cfg).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err)
	delivered := decodeSends(t, sender.captured())
	require.Len(t, delivered, 5, "opened, comment, bot, approve, merge")
	assert.Equal(t, "User", delivered[1].SenderType)
	assert.Equal(t, "Bot", delivered[2].SenderType)

	assert.Contains(t, reactionSteps(result), "bot")
	for _, check := range result.Reactions {
		if check.Step != "bot" {
			continue
		}
		assert.Equal(t, "robot_face", check.Emoji)
		assert.True(t, check.Present)
		assert.NoError(t, check.VerifyErr)
	}
}

func TestSmokeRun_WithReactions_IgnoreAIReviews_SkipsBotStep(t *testing.T) {
	sender := &fakeSender{}
	cfg := defaultConfig()
	cfg.IgnoreAIReviews = true
	cfg.Reactions.BotReview = "robot_face"
	reactions := &fakeReactions{names: []string{testReactions.Commented, testReactions.Approved, testReactions.MergedPR}}

	result, err := newSmoke(sender, &fakeMessages{ts: testTS}, reactions, &fakeCleanup{}, cfg).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err)
	assert.Len(t, sender.captured(), 4, "the bot step is skipped when AI reviews are ignored")
	assert.NotContains(t, reactionSteps(result), "bot")
	assert.True(t, result.IgnoreAIReviews, "the CLI needs the flag to explain the skip")
}

func TestSmokeRun_WithReactions_EmptyBotMarker_SkipsBotStep(t *testing.T) {
	sender := &fakeSender{}
	reactions := &fakeReactions{names: []string{testReactions.Commented, testReactions.Approved, testReactions.MergedPR}}

	// testReactions leaves BotReview empty — the operator's off-switch.
	result, err := newSmoke(sender, &fakeMessages{ts: testTS}, reactions, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err)
	assert.Len(t, sender.captured(), 4)
	assert.NotContains(t, reactionSteps(result), "bot")
	assert.Empty(t, result.BotReviewMarker)
}

func TestSmokeRun_WithReactions_MissingEmoji_RecordedNotPresent(t *testing.T) {
	sender := &fakeSender{}
	reactions := &fakeReactions{names: nil} // the server "added" nothing

	result, err := newSmoke(sender, &fakeMessages{ts: testTS}, reactions, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err, "a missing emoji is reported in the result, not as an error")
	require.NotEmpty(t, result.Reactions)
	for _, check := range result.Reactions {
		assert.False(t, check.Present, "step %s", check.Step)
		assert.NoError(t, check.VerifyErr, "step %s", check.Step)
	}
}

func TestSmokeRun_WithReactions_VerifyError_RecordedAsErr(t *testing.T) {
	sender := &fakeSender{}
	verifyErr := errors.New("missing_scope: reactions:read")
	reactions := &fakeReactions{err: verifyErr}

	result, err := newSmoke(sender, &fakeMessages{ts: testTS}, reactions, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err, "a verify error degrades gracefully")
	require.NotEmpty(t, result.Reactions)
	for _, check := range result.Reactions {
		assert.ErrorIs(t, check.VerifyErr, verifyErr, "step %s", check.Step)
	}
}

func TestSmokeRun_ReactionsFlagButDisabledInConfig_SkipsLifecycle(t *testing.T) {
	sender := &fakeSender{}
	reactions := &fakeReactions{}
	cfg := defaultConfig()
	cfg.Reactions.Enabled = false

	result, err := newSmoke(sender, &fakeMessages{ts: testTS}, reactions, &fakeCleanup{}, cfg).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err)
	assert.Len(t, sender.captured(), 1, "reactions disabled means the open event only")
	assert.Zero(t, reactions.calls)
	assert.True(t, result.ReactionsRequested)
	assert.False(t, result.ReactionsEnabled)
	assert.Empty(t, result.Reactions)
}

func TestSmokeRun_UnmappedRepo_FailsBeforeAnyNetworkCall(t *testing.T) {
	sender := &fakeSender{}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), "nope/missing", true)

	require.ErrorIs(t, err, diagnosticsdomain.ErrNoMapping)
	assert.Empty(t, sender.captured(), "an unmapped repo never reaches the network")
}

func TestSmokeRun_BadSecret_ReportsSignatureRejected(t *testing.T) {
	sender := &fakeSender{statusCode: 401}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, false)

	assert.ErrorIs(t, err, diagnosticsdomain.ErrSignatureRejected)
}

func TestSmokeRun_TransportError_ReportsUnreachable(t *testing.T) {
	sender := &fakeSender{transportErr: errors.New("connection refused")}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, false)

	assert.ErrorIs(t, err, diagnosticsdomain.ErrUnreachable)
}

func TestSmokeRun_UnexpectedStatus_ReportsUnexpectedStatus(t *testing.T) {
	sender := &fakeSender{statusCode: 500}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, &fakeCleanup{}, defaultConfig()).
		Run(context.Background(), testRepo, false)

	assert.ErrorIs(t, err, diagnosticsdomain.ErrUnexpectedStatus)
}

func TestSmokeRun_DeletesSyntheticRow_OnSuccess(t *testing.T) {
	cleanup := &fakeCleanup{}

	result, err := newSmoke(&fakeSender{}, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultConfig()).
		Run(context.Background(), testRepo, false)

	require.NoError(t, err)
	require.True(t, cleanup.deleteCalled, "the synthetic pull_requests row would otherwise be orphaned")
	assert.Equal(t, testRepo, cleanup.deletedRepo)
	assert.Equal(t, result.PRNumber, cleanup.deletedNumber)
}

func TestSmokeRun_DeletesSyntheticRow_WithReactions(t *testing.T) {
	cleanup := &fakeCleanup{}
	reactions := &fakeReactions{names: []string{testReactions.Commented, testReactions.Approved, testReactions.MergedPR}}

	result, err := newSmoke(&fakeSender{}, &fakeMessages{ts: testTS}, reactions, cleanup, defaultConfig()).
		Run(context.Background(), testRepo, true)

	require.NoError(t, err)
	require.True(t, cleanup.deleteCalled)
	assert.Equal(t, testRepo, cleanup.deletedRepo)
	assert.Equal(t, result.PRNumber, cleanup.deletedNumber)
}

func TestSmokeRun_CleanupFailure_IsReported(t *testing.T) {
	cleanupErr := errors.New("db is locked")
	cleanup := &fakeCleanup{deleteErr: cleanupErr}

	_, err := newSmoke(&fakeSender{}, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultConfig()).
		Run(context.Background(), testRepo, false)

	assert.ErrorIs(t, err, cleanupErr)
}

func TestSmokeRun_SignerReceivesSecret(t *testing.T) {
	signer := &fakeSigner{}
	smoke := application.NewSmokeUseCase(
		fakeMappings{repo: testRepo, channel: testChannel},
		&fakeMessages{ts: testTS},
		&fakeReactions{},
		&fakeCleanup{},
		signer,
		fakeWebhookBuilder{},
		&fakeSender{},
		defaultConfig(),
	)

	_, err := smoke.Run(context.Background(), testRepo, false)

	require.NoError(t, err)
	signer.mu.Lock()
	defer signer.mu.Unlock()
	assert.Equal(t, testSecret, signer.secret)
	assert.NotEmpty(t, signer.signed, "every delivery is signed")
}
