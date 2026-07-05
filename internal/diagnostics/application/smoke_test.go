package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/diagnostics/application"
	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// ---- constants -----

const (
	testSecret  = "topsecret"
	testRepo    = "octo/widget"
	testChannel = "C0123ABCDE"
	testTS      = "1717171717.000100"
)

var testRxCfg = diagnosticsdomain.SmokeReactionsConfig{
	Enabled:       true,
	NewPR:         "large_green_circle",
	MergedPR:      "twisted_rightwards_arrows",
	Approved:      "white_check_mark",
	Commented:     "speech_balloon",
	RequestChange: "exclamation",
}

// ---- fake ports -----

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

func copyHeaders(h map[string]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = v
	}
	return out
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

func (fakeWebhookBuilder) Build(_ string, number int, title string, ev diagnosticsdomain.SmokeEvent) (diagnosticsdomain.ForgedWebhook, error) {
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
	switch ev.Kind {
	case diagnosticsdomain.SmokeOpened:
		payload.Action = "opened"
	case diagnosticsdomain.SmokeCommented:
		eventValue = "pull_request_review"
		payload.Action = "submitted"
		payload.Review = &review{State: "commented"}
		if ev.IsBot {
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

// ---- helpers -----

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1717171717, 0) }
}

func defaultCfg(url string) diagnosticsdomain.SmokeConfig {
	return diagnosticsdomain.SmokeConfig{
		WebhookURL:      url,
		WebhookSecret:   testSecret,
		IgnoreAIReviews: false,
		Reactions:       testRxCfg,
		Now:             fixedClock(),
	}
}

func newSmoke(sender *fakeSender, msgs *fakeMessages, rx *fakeReactions, cleanup *fakeCleanup, cfg diagnosticsdomain.SmokeConfig) *application.SmokeUseCase {
	signer := &fakeSigner{}
	builder := fakeWebhookBuilder{}
	return application.NewSmokeUseCase(
		fakeMappings{repo: testRepo, channel: testChannel},
		msgs,
		rx,
		cleanup,
		signer,
		builder,
		sender,
		cfg,
	)
}

// decodeEvent pulls routing-relevant fields from a captured body.
func decodeEvent(t *testing.T, body []byte) (action, reviewState string, merged bool, number int, title string) {
	t.Helper()
	var p struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Merged bool   `json:"merged"`
		} `json:"pull_request"`
		Review *struct {
			State string `json:"state"`
		} `json:"review"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	state := ""
	if p.Review != nil {
		state = p.Review.State
	}
	return p.Action, state, p.PullRequest.Merged, p.PullRequest.Number, p.PullRequest.Title
}

func decodeSenderType(t *testing.T, body []byte) string {
	t.Helper()
	var p struct {
		Sender struct {
			Type string `json:"type"`
		} `json:"sender"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	return p.Sender.Type
}

// ---- tests -----

func TestSmokeRun_OpenedDelivery_DrivesEndpointAndReportsChannelAndTS(t *testing.T) {
	sender := &fakeSender{}
	msgs := &fakeMessages{ts: testTS}
	rx := &fakeReactions{}
	cleanup := &fakeCleanup{}

	res, err := newSmoke(sender, msgs, rx, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, false)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}

	sends := sender.captured()
	if len(sends) != 1 {
		t.Fatalf("got %d sends; want 1 (opened only, no --reactions)", len(sends))
	}
	action, _, _, number, title := decodeEvent(t, sends[0].body)
	if action != "opened" {
		t.Errorf("action = %q; want opened", action)
	}
	if !strings.Contains(title, "[notifycat smoke]") {
		t.Errorf("title %q is not marked as a smoke test", title)
	}
	if sends[0].headers["X-GitHub-Event"] != "pull_request" {
		t.Errorf("X-GitHub-Event = %q; want pull_request", sends[0].headers["X-GitHub-Event"])
	}
	if sends[0].headers["X-Hub-Signature-256"] == "" {
		t.Error("signature header missing from sent request")
	}
	if rx.calls != 0 {
		t.Errorf("Reactions called %d times without --reactions; want 0", rx.calls)
	}
	if res.Channel != testChannel || res.Timestamp != testTS {
		t.Errorf("Result channel/ts = %q/%q; want %q/%q", res.Channel, res.Timestamp, testChannel, testTS)
	}
	if msgs.gotNumber != number {
		t.Errorf("Messages number = %d; want %d", msgs.gotNumber, number)
	}
	if len(res.Reactions) != 0 {
		t.Errorf("Result.Reactions = %+v; want empty without --reactions", res.Reactions)
	}
}

func TestSmokeRun_WithReactions_RunsLifecycleAndVerifiesEachEmoji(t *testing.T) {
	sender := &fakeSender{}
	rx := &fakeReactions{names: []string{testRxCfg.Commented, testRxCfg.Approved, testRxCfg.MergedPR}}
	cleanup := &fakeCleanup{}

	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}

	sends := sender.captured()
	if len(sends) != 4 {
		t.Fatalf("got %d sends; want 4 (opened, comment, approve, merge)", len(sends))
	}
	if a, _, _, _, _ := decodeEvent(t, sends[0].body); sends[0].headers["X-GitHub-Event"] != "pull_request" || a != "opened" {
		t.Errorf("send0 = %s/%s; want pull_request/opened", sends[0].headers["X-GitHub-Event"], a)
	}
	if a, s, _, _, _ := decodeEvent(t, sends[1].body); sends[1].headers["X-GitHub-Event"] != "pull_request_review" || a != "submitted" || s != "commented" {
		t.Errorf("send1 = %s/%s/%s; want pull_request_review/submitted/commented", sends[1].headers["X-GitHub-Event"], a, s)
	}
	if a, s, _, _, _ := decodeEvent(t, sends[2].body); sends[2].headers["X-GitHub-Event"] != "pull_request_review" || a != "submitted" || s != "approved" {
		t.Errorf("send2 = %s/%s/%s; want pull_request_review/submitted/approved", sends[2].headers["X-GitHub-Event"], a, s)
	}
	if a, _, merged, _, _ := decodeEvent(t, sends[3].body); sends[3].headers["X-GitHub-Event"] != "pull_request" || a != "closed" || !merged {
		t.Errorf("send3 = %s/%s/merged=%v; want pull_request/closed/merged=true", sends[3].headers["X-GitHub-Event"], a, merged)
	}

	// All events share the same PR number.
	_, _, _, prNumber, _ := decodeEvent(t, sends[0].body)
	for i, send := range sends {
		if _, _, _, n, _ := decodeEvent(t, send.body); n != prNumber {
			t.Errorf("send%d PR number = %d; want %d", i, n, prNumber)
		}
	}

	want := []struct{ step, emoji string }{
		{"comment", testRxCfg.Commented},
		{"approve", testRxCfg.Approved},
		{"merge", testRxCfg.MergedPR},
	}
	if len(res.Reactions) != len(want) {
		t.Fatalf("Result.Reactions has %d entries; want %d", len(res.Reactions), len(want))
	}
	for i, w := range want {
		c := res.Reactions[i]
		if c.Step != w.step || c.Emoji != w.emoji || !c.Present || c.VerifyErr != nil {
			t.Errorf("Reactions[%d] = %+v; want step=%s emoji=%s present=true err=nil", i, c, w.step, w.emoji)
		}
	}
}

func TestSmokeRun_WithReactions_BotMarkerConfigured_ReplaysBotReviewAndVerifiesMarker(t *testing.T) {
	sender := &fakeSender{}
	rxCfg := testRxCfg
	rxCfg.BotReview = "robot_face"
	rx := &fakeReactions{names: []string{rxCfg.Commented, rxCfg.BotReview, rxCfg.Approved, rxCfg.MergedPR}}
	cleanup := &fakeCleanup{}

	cfg := defaultCfg("http://fake")
	cfg.Reactions = rxCfg
	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, cfg).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}

	sends := sender.captured()
	if len(sends) != 5 {
		t.Fatalf("got %d sends; want 5 (opened, comment, bot, approve, merge)", len(sends))
	}
	if got := decodeSenderType(t, sends[2].body); got != "Bot" {
		t.Errorf("bot step sender.type = %q; want Bot", got)
	}
	if got := decodeSenderType(t, sends[1].body); got != "User" {
		t.Errorf("human comment step sender.type = %q; want User", got)
	}

	var bot *diagnosticsdomain.SmokeReactionCheck
	for i := range res.Reactions {
		if res.Reactions[i].Step == "bot" {
			bot = &res.Reactions[i]
		}
	}
	if bot == nil {
		t.Fatalf("no bot step recorded in Result.Reactions: %+v", res.Reactions)
	}
	if bot.Emoji != "robot_face" || !bot.Present || bot.VerifyErr != nil {
		t.Errorf("bot step = %+v; want emoji=robot_face present=true err=nil", *bot)
	}
}

func TestSmokeRun_WithReactions_IgnoreAIReviews_SkipsBotStep(t *testing.T) {
	sender := &fakeSender{}
	rxCfg := testRxCfg
	rxCfg.BotReview = "robot_face"
	rx := &fakeReactions{names: []string{rxCfg.Commented, rxCfg.Approved, rxCfg.MergedPR}}
	cleanup := &fakeCleanup{}

	cfg := defaultCfg("http://fake")
	cfg.IgnoreAIReviews = true
	cfg.Reactions = rxCfg
	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, cfg).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if got := len(sender.captured()); got != 4 {
		t.Fatalf("got %d sends; want 4 (bot step skipped when AI reviews are ignored)", got)
	}
	for _, c := range res.Reactions {
		if c.Step == "bot" {
			t.Errorf("bot step replayed despite IgnoreAIReviews: %+v", res.Reactions)
		}
	}
	if !res.IgnoreAIReviews {
		t.Error("Result.IgnoreAIReviews = false; want true so the CLI can explain the skip")
	}
}

func TestSmokeRun_WithReactions_EmptyBotMarker_SkipsBotStep(t *testing.T) {
	sender := &fakeSender{}
	rx := &fakeReactions{names: []string{testRxCfg.Commented, testRxCfg.Approved, testRxCfg.MergedPR}}
	cleanup := &fakeCleanup{}

	// testRxCfg has BotReview == "" — the operator's off-switch.
	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if got := len(sender.captured()); got != 4 {
		t.Fatalf("got %d sends; want 4 (no bot step when the marker is empty)", got)
	}
	for _, c := range res.Reactions {
		if c.Step == "bot" {
			t.Error("bot step replayed with an empty marker")
		}
	}
	if res.BotReviewMarker != "" {
		t.Errorf("Result.BotReviewMarker = %q; want empty", res.BotReviewMarker)
	}
}

func TestSmokeRun_WithReactions_MissingEmoji_RecordedNotPresent(t *testing.T) {
	sender := &fakeSender{}
	rx := &fakeReactions{names: nil} // server "added" nothing
	cleanup := &fakeCleanup{}

	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil (missing emoji is reported in Result, not an error)", err)
	}
	for _, c := range res.Reactions {
		if c.Present || c.VerifyErr != nil {
			t.Errorf("Reactions[%s] = %+v; want present=false err=nil", c.Step, c)
		}
	}
}

func TestSmokeRun_WithReactions_VerifyError_RecordedAsErr(t *testing.T) {
	sender := &fakeSender{}
	sentinel := errors.New("missing_scope: reactions:read")
	rx := &fakeReactions{err: sentinel}
	cleanup := &fakeCleanup{}

	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil (a verify error degrades gracefully)", err)
	}
	for _, c := range res.Reactions {
		if c.VerifyErr == nil {
			t.Errorf("Reactions[%s].VerifyErr = nil; want the Reactions error", c.Step)
		}
	}
}

func TestSmokeRun_ReactionsFlagButDisabledInConfig_SkipsLifecycle(t *testing.T) {
	sender := &fakeSender{}
	rx := &fakeReactions{}
	cleanup := &fakeCleanup{}

	cfg := defaultCfg("http://fake")
	cfg.Reactions.Enabled = false
	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, cfg).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if got := len(sender.captured()); got != 1 {
		t.Errorf("got %d sends; want 1 (reactions disabled → opened only)", got)
	}
	if rx.calls != 0 {
		t.Errorf("Reactions called %d times; want 0 when reactions are disabled", rx.calls)
	}
	if !res.ReactionsRequested || res.ReactionsEnabled {
		t.Errorf("Result requested=%v enabled=%v; want requested=true enabled=false", res.ReactionsRequested, res.ReactionsEnabled)
	}
	if len(res.Reactions) != 0 {
		t.Errorf("Result.Reactions = %+v; want empty when disabled", res.Reactions)
	}
}

func TestSmokeRun_UnmappedRepo_FailsBeforeAnyNetworkCall(t *testing.T) {
	sender := &fakeSender{}
	cleanup := &fakeCleanup{}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultCfg("http://fake")).Run(context.Background(), "nope/missing", true)
	if !errors.Is(err, diagnosticsdomain.ErrNoMapping) {
		t.Fatalf("Run error = %v; want ErrNoMapping", err)
	}
	if len(sender.captured()) != 0 {
		t.Error("sender was called for an unmapped repo; want no network call")
	}
}

func TestSmokeRun_BadSecret_ReportsSignatureRejected(t *testing.T) {
	sender := &fakeSender{statusCode: 401}
	cleanup := &fakeCleanup{}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, false)
	if !errors.Is(err, diagnosticsdomain.ErrSignatureRejected) {
		t.Fatalf("Run error = %v; want ErrSignatureRejected", err)
	}
}

func TestSmokeRun_TransportError_ReportsUnreachable(t *testing.T) {
	transportErr := errors.New("connection refused")
	sender := &fakeSender{transportErr: transportErr}
	cleanup := &fakeCleanup{}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, false)
	if !errors.Is(err, diagnosticsdomain.ErrUnreachable) {
		t.Fatalf("Run error = %v; want ErrUnreachable", err)
	}
}

func TestSmokeRun_UnexpectedStatus_ReportsUnexpectedStatus(t *testing.T) {
	sender := &fakeSender{statusCode: 500}
	cleanup := &fakeCleanup{}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, false)
	if !errors.Is(err, diagnosticsdomain.ErrUnexpectedStatus) {
		t.Fatalf("Run error = %v; want ErrUnexpectedStatus", err)
	}
}

func TestSmokeRun_DeletesSyntheticRow_OnSuccess(t *testing.T) {
	sender := &fakeSender{}
	cleanup := &fakeCleanup{}

	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, false)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if !cleanup.deleteCalled {
		t.Fatal("DeletePR was not called; the synthetic pull_requests row would be orphaned")
	}
	if cleanup.deletedRepo != testRepo || cleanup.deletedNumber != res.PRNumber {
		t.Errorf("DeletePR(%q, %d); want (%q, %d)", cleanup.deletedRepo, cleanup.deletedNumber, testRepo, res.PRNumber)
	}
}

func TestSmokeRun_DeletesSyntheticRow_WithReactions(t *testing.T) {
	sender := &fakeSender{}
	rx := &fakeReactions{names: []string{testRxCfg.Commented, testRxCfg.Approved, testRxCfg.MergedPR}}
	cleanup := &fakeCleanup{}

	res, err := newSmoke(sender, &fakeMessages{ts: testTS}, rx, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if !cleanup.deleteCalled || cleanup.deletedRepo != testRepo || cleanup.deletedNumber != res.PRNumber {
		t.Errorf("DeletePR called=%v (%q, %d); want true (%q, %d)", cleanup.deleteCalled, cleanup.deletedRepo, cleanup.deletedNumber, testRepo, res.PRNumber)
	}
}

func TestSmokeRun_CleanupFailure_IsReported(t *testing.T) {
	sender := &fakeSender{}
	sentinel := errors.New("db is locked")
	cleanup := &fakeCleanup{deleteErr: sentinel}

	_, err := newSmoke(sender, &fakeMessages{ts: testTS}, &fakeReactions{}, cleanup, defaultCfg("http://fake")).Run(context.Background(), testRepo, false)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Run error = %v; want it to wrap the cleanup failure", err)
	}
}

func TestSmokeRun_SignerReceivesSecret(t *testing.T) {
	signer := &fakeSigner{}
	sender := &fakeSender{}
	cleanup := &fakeCleanup{}

	cfg := defaultCfg("http://fake")
	uc := application.NewSmokeUseCase(
		fakeMappings{repo: testRepo, channel: testChannel},
		&fakeMessages{ts: testTS},
		&fakeReactions{},
		cleanup,
		signer,
		fakeWebhookBuilder{},
		sender,
		cfg,
	)
	if _, err := uc.Run(context.Background(), testRepo, false); err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	signer.mu.Lock()
	defer signer.mu.Unlock()
	if signer.secret != testSecret {
		t.Errorf("signer received webhook secret %q; want %q", signer.secret, testSecret)
	}
	if len(signer.signed) == 0 {
		t.Error("signer was never called")
	}
}
