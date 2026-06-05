package smoke_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/githubhook"
	"github.com/mptooling/notifycat/internal/slack"
	"github.com/mptooling/notifycat/internal/smoke"
	"github.com/mptooling/notifycat/internal/store"
)

const (
	testSecret  = "topsecret"
	testRepo    = "octo/widget"
	testChannel = "C0123ABCDE"
	testTS      = "1717171717.000100"
)

var testReactions = config.Reactions{
	Enabled:       true,
	NewPR:         "large_green_circle",
	MergedPR:      "twisted_rightwards_arrows",
	ClosedPR:      "x",
	Approved:      "white_check_mark",
	Commented:     "speech_balloon",
	RequestChange: "exclamation",
}

// fakeMappings answers Get for exactly one repository.
type fakeMappings struct {
	repo    string
	channel string
}

func (f fakeMappings) Get(_ context.Context, repository string) (store.RepoMapping, error) {
	if repository != f.repo {
		return store.RepoMapping{}, store.ErrNotFound
	}
	return store.RepoMapping{Repository: repository, SlackChannel: f.channel}, nil
}

// fakeStore returns a fixed message, or ErrNotFound when ts is empty.
type fakeStore struct {
	ts        string
	gotRepo   string
	gotNumber int
}

func (f *fakeStore) Get(_ context.Context, repository string, prNumber int) (store.SlackMessage, error) {
	f.gotRepo = repository
	f.gotNumber = prNumber
	if f.ts == "" {
		return store.SlackMessage{}, store.ErrNotFound
	}
	return store.SlackMessage{Repository: repository, PRNumber: prNumber, TS: f.ts}, nil
}

// fakeReactions stands in for the Slack reactions.get call.
type fakeReactions struct {
	reactions []slack.Reaction
	err       error
	calls     int
}

func (f *fakeReactions) GetReactions(_ context.Context, _, _ string) ([]slack.Reaction, error) {
	f.calls++
	return f.reactions, f.err
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1717171717, 0) }
}

// capturedReq records one inbound webhook for later assertions.
type capturedReq struct {
	event string
	sig   string
	body  []byte
}

// recordingServer returns an httptest server that records every request and
// answers 200, plus a function to read the captured requests race-safely.
func recordingServer(t *testing.T) (*httptest.Server, func() []capturedReq) {
	t.Helper()
	var mu sync.Mutex
	var reqs []capturedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, capturedReq{
			event: r.Header.Get("X-GitHub-Event"),
			sig:   r.Header.Get(githubhook.SignatureHeader),
			body:  body,
		})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"ok"`))
	}))
	return srv, func() []capturedReq {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedReq(nil), reqs...)
	}
}

func newSmoke(t *testing.T, url string, st smoke.MessageStore, rx smoke.ReactionReader, rxCfg config.Reactions) *smoke.Smoke {
	t.Helper()
	return newSmokeAI(t, url, st, rx, rxCfg, false)
}

// newSmokeAI is newSmoke with an explicit NOTIFYCAT_IGNORE_AI_REVIEWS value, for
// exercising the bot-review marker step.
func newSmokeAI(t *testing.T, url string, st smoke.MessageStore, rx smoke.ReactionReader, rxCfg config.Reactions, ignoreAIReviews bool) *smoke.Smoke {
	t.Helper()
	return smoke.New(
		fakeMappings{repo: testRepo, channel: testChannel},
		st,
		rx,
		http.DefaultClient,
		testSecret,
		url,
		rxCfg,
		ignoreAIReviews,
		fixedClock(),
	)
}

// decodeEvent pulls the routing-relevant fields out of a captured body.
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

// decodeSenderType pulls sender.type out of a captured body.
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

func TestRun_OpenedDelivery_DrivesRealEndpointAndReportsChannelAndTS(t *testing.T) {
	srv, captured := recordingServer(t)
	defer srv.Close()

	st := &fakeStore{ts: testTS}
	rx := &fakeReactions{}
	res, err := newSmoke(t, srv.URL, st, rx, testReactions).Run(context.Background(), testRepo, false)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}

	reqs := captured()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests; want exactly 1 (opened only, no --reactions)", len(reqs))
	}
	if reqs[0].event != "pull_request" {
		t.Errorf("X-GitHub-Event = %q; want pull_request", reqs[0].event)
	}
	if err := githubhook.NewVerifier(testSecret).Verify(reqs[0].body, reqs[0].sig); err != nil {
		t.Errorf("server could not verify signature: %v", err)
	}
	action, _, _, number, title := decodeEvent(t, reqs[0].body)
	if action != "opened" {
		t.Errorf("action = %q; want opened", action)
	}
	if !strings.Contains(title, "[notifycat smoke]") {
		t.Errorf("title %q is not marked as a smoke test", title)
	}
	if rx.calls != 0 {
		t.Errorf("GetReactions called %d times without --reactions; want 0", rx.calls)
	}
	if res.Channel != testChannel || res.Timestamp != testTS {
		t.Errorf("Result channel/ts = %q/%q; want %q/%q", res.Channel, res.Timestamp, testChannel, testTS)
	}
	if st.gotNumber != number {
		t.Errorf("store.Get number = %d; want %d", st.gotNumber, number)
	}
	if len(res.Reactions) != 0 {
		t.Errorf("Result.Reactions = %+v; want empty without --reactions", res.Reactions)
	}
}

func TestRun_WithReactions_RunsLifecycleAndVerifiesEachEmoji(t *testing.T) {
	srv, captured := recordingServer(t)
	defer srv.Close()

	rx := &fakeReactions{reactions: []slack.Reaction{
		{Name: testReactions.Commented},
		{Name: testReactions.Approved},
		{Name: testReactions.MergedPR},
	}}
	res, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, rx, testReactions).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}

	reqs := captured()
	if len(reqs) != 4 {
		t.Fatalf("got %d requests; want 4 (opened, comment, approve, merge)", len(reqs))
	}
	// opened
	if a, _, _, _, _ := decodeEvent(t, reqs[0].body); reqs[0].event != "pull_request" || a != "opened" {
		t.Errorf("req0 = %s/%s; want pull_request/opened", reqs[0].event, a)
	}
	// comment
	if a, s, _, _, _ := decodeEvent(t, reqs[1].body); reqs[1].event != "pull_request_review" || a != "submitted" || s != "commented" {
		t.Errorf("req1 = %s/%s/%s; want pull_request_review/submitted/commented", reqs[1].event, a, s)
	}
	// approve
	if a, s, _, _, _ := decodeEvent(t, reqs[2].body); reqs[2].event != "pull_request_review" || a != "submitted" || s != "approved" {
		t.Errorf("req2 = %s/%s/%s; want pull_request_review/submitted/approved", reqs[2].event, a, s)
	}
	// merge
	if a, _, merged, _, _ := decodeEvent(t, reqs[3].body); reqs[3].event != "pull_request" || a != "closed" || !merged {
		t.Errorf("req3 = %s/%s/merged=%v; want pull_request/closed/merged=true", reqs[3].event, a, merged)
	}

	// every event reuses the same PR number so reactions land on one message.
	_, _, _, n0, _ := decodeEvent(t, reqs[0].body)
	for i, r := range reqs {
		if _, _, _, n, _ := decodeEvent(t, r.body); n != n0 {
			t.Errorf("req%d PR number = %d; want %d (shared across the lifecycle)", i, n, n0)
		}
		if err := githubhook.NewVerifier(testSecret).Verify(r.body, r.sig); err != nil {
			t.Errorf("req%d signature did not verify: %v", i, err)
		}
	}

	want := []struct{ step, emoji string }{
		{"comment", testReactions.Commented},
		{"approve", testReactions.Approved},
		{"merge", testReactions.MergedPR},
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

func TestRun_WithReactions_BotMarkerConfigured_ReplaysBotReviewAndVerifiesMarker(t *testing.T) {
	srv, captured := recordingServer(t)
	defer srv.Close()

	rxCfg := testReactions
	rxCfg.BotReview = "robot_face"
	rx := &fakeReactions{reactions: []slack.Reaction{
		{Name: rxCfg.Commented}, {Name: rxCfg.BotReview}, {Name: rxCfg.Approved}, {Name: rxCfg.MergedPR},
	}}
	res, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, rx, rxCfg).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}

	reqs := captured()
	if len(reqs) != 5 {
		t.Fatalf("got %d requests; want 5 (opened, comment, bot, approve, merge)", len(reqs))
	}
	// The bot step (req2) is a commented review from a Bot sender; the human
	// comment step (req1) stays a User so the two are genuinely distinct.
	if reqs[2].event != "pull_request_review" {
		t.Errorf("req2 event = %q; want pull_request_review", reqs[2].event)
	}
	if got := decodeSenderType(t, reqs[2].body); got != "Bot" {
		t.Errorf("bot step sender.type = %q; want Bot", got)
	}
	if got := decodeSenderType(t, reqs[1].body); got != "User" {
		t.Errorf("human comment step sender.type = %q; want User", got)
	}

	var bot *smoke.ReactionCheck
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

func TestRun_WithReactions_IgnoreAIReviews_SkipsBotStep(t *testing.T) {
	srv, captured := recordingServer(t)
	defer srv.Close()

	rxCfg := testReactions
	rxCfg.BotReview = "robot_face" // configured, but muted by the ignore flag
	rx := &fakeReactions{reactions: []slack.Reaction{
		{Name: rxCfg.Commented}, {Name: rxCfg.Approved}, {Name: rxCfg.MergedPR},
	}}
	res, err := newSmokeAI(t, srv.URL, &fakeStore{ts: testTS}, rx, rxCfg, true).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if got := len(captured()); got != 4 {
		t.Fatalf("got %d requests; want 4 (bot step skipped when AI reviews are ignored)", got)
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

func TestRun_WithReactions_EmptyBotMarker_SkipsBotStep(t *testing.T) {
	srv, captured := recordingServer(t)
	defer srv.Close()

	// testReactions leaves BotReview empty — the operator's off-switch.
	rx := &fakeReactions{reactions: []slack.Reaction{
		{Name: testReactions.Commented}, {Name: testReactions.Approved}, {Name: testReactions.MergedPR},
	}}
	res, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, rx, testReactions).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if got := len(captured()); got != 4 {
		t.Fatalf("got %d requests; want 4 (no bot step when the marker is empty)", got)
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

func TestRun_WithReactions_MissingEmoji_RecordedNotPresent(t *testing.T) {
	srv, _ := recordingServer(t)
	defer srv.Close()

	rx := &fakeReactions{reactions: nil} // server "added" nothing
	res, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, rx, testReactions).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil (missing emoji is reported in Result, not an error)", err)
	}
	for _, c := range res.Reactions {
		if c.Present || c.VerifyErr != nil {
			t.Errorf("Reactions[%s] = %+v; want present=false err=nil", c.Step, c)
		}
	}
}

func TestRun_WithReactions_VerifyError_RecordedAsErr(t *testing.T) {
	srv, _ := recordingServer(t)
	defer srv.Close()

	sentinel := errors.New("missing_scope: reactions:read")
	rx := &fakeReactions{err: sentinel}
	res, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, rx, testReactions).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil (a verify error degrades gracefully)", err)
	}
	for _, c := range res.Reactions {
		if c.VerifyErr == nil {
			t.Errorf("Reactions[%s].VerifyErr = nil; want the GetReactions error", c.Step)
		}
	}
}

func TestRun_ReactionsFlagButDisabledInConfig_SkipsLifecycle(t *testing.T) {
	srv, captured := recordingServer(t)
	defer srv.Close()

	disabled := testReactions
	disabled.Enabled = false
	rx := &fakeReactions{}
	res, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, rx, disabled).Run(context.Background(), testRepo, true)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}
	if got := len(captured()); got != 1 {
		t.Errorf("got %d requests; want 1 (reactions disabled in config → opened only)", got)
	}
	if rx.calls != 0 {
		t.Errorf("GetReactions called %d times; want 0 when reactions are disabled", rx.calls)
	}
	if !res.ReactionsRequested || res.ReactionsEnabled {
		t.Errorf("Result requested=%v enabled=%v; want requested=true enabled=false", res.ReactionsRequested, res.ReactionsEnabled)
	}
	if len(res.Reactions) != 0 {
		t.Errorf("Result.Reactions = %+v; want empty when disabled", res.Reactions)
	}
}

func TestRun_UnmappedRepo_FailsBeforeAnyNetworkCall(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer srv.Close()

	_, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, &fakeReactions{}, testReactions).Run(context.Background(), "nope/missing", true)
	if !errors.Is(err, smoke.ErrNoMapping) {
		t.Fatalf("Run error = %v; want ErrNoMapping", err)
	}
	if hit {
		t.Error("server was contacted for an unmapped repo; want no network call")
	}
}

func TestRun_BadSecret_ReportsSignatureRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}, &fakeReactions{}, testReactions).Run(context.Background(), testRepo, false)
	if !errors.Is(err, smoke.ErrSignatureRejected) {
		t.Fatalf("Run error = %v; want ErrSignatureRejected", err)
	}
}

func TestRun_ServerUnreachable_ReportsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := newSmoke(t, url, &fakeStore{ts: testTS}, &fakeReactions{}, testReactions).Run(context.Background(), testRepo, false)
	if !errors.Is(err, smoke.ErrUnreachable) {
		t.Fatalf("Run error = %v; want ErrUnreachable", err)
	}
}
