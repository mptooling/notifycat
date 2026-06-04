package smoke_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mptooling/notifycat/internal/githubhook"
	"github.com/mptooling/notifycat/internal/smoke"
	"github.com/mptooling/notifycat/internal/store"
)

const (
	testSecret  = "topsecret"
	testRepo    = "octo/widget"
	testChannel = "C0123ABCDE"
	testTS      = "1717171717.000100"
)

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

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1717171717, 0) }
}

func newSmoke(t *testing.T, url string, st smoke.MessageStore) *smoke.Smoke {
	t.Helper()
	return smoke.New(
		fakeMappings{repo: testRepo, channel: testChannel},
		st,
		http.DefaultClient,
		testSecret,
		url,
		fixedClock(),
	)
}

func TestRun_HappyPath_DrivesRealEndpointAndReportsChannelAndTS(t *testing.T) {
	var gotEvent, gotSig string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEvent = r.Header.Get("X-GitHub-Event")
		gotSig = r.Header.Get(githubhook.SignatureHeader)
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`"ok"`))
	}))
	defer srv.Close()

	st := &fakeStore{ts: testTS}
	res, err := newSmoke(t, srv.URL, st).Run(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("Run returned %v; want nil", err)
	}

	if gotEvent != "pull_request" {
		t.Errorf("X-GitHub-Event = %q; want pull_request", gotEvent)
	}
	if err := githubhook.NewVerifier(testSecret).Verify(gotBody, gotSig); err != nil {
		t.Errorf("server could not verify signature: %v", err)
	}

	var payload struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Draft  bool   `json:"draft"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v", err)
	}
	if payload.Action != "opened" {
		t.Errorf("action = %q; want opened", payload.Action)
	}
	if payload.Repository.FullName != testRepo {
		t.Errorf("repository.full_name = %q; want %q", payload.Repository.FullName, testRepo)
	}
	if payload.PullRequest.Draft {
		t.Error("payload draft = true; opened-draft would be ignored by the server")
	}
	if !strings.Contains(payload.PullRequest.Title, "[notifycat smoke]") {
		t.Errorf("title %q is not marked as a smoke test", payload.PullRequest.Title)
	}

	if res.Channel != testChannel {
		t.Errorf("Channel = %q; want %q", res.Channel, testChannel)
	}
	if res.Timestamp != testTS {
		t.Errorf("Timestamp = %q; want %q", res.Timestamp, testTS)
	}
	if st.gotRepo != testRepo || st.gotNumber != payload.PullRequest.Number {
		t.Errorf("store.Get(%q, %d); want (%q, %d)", st.gotRepo, st.gotNumber, testRepo, payload.PullRequest.Number)
	}
}

func TestRun_UnmappedRepo_FailsBeforeAnyNetworkCall(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer srv.Close()

	_, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}).Run(context.Background(), "nope/missing")
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

	_, err := newSmoke(t, srv.URL, &fakeStore{ts: testTS}).Run(context.Background(), testRepo)
	if !errors.Is(err, smoke.ErrSignatureRejected) {
		t.Fatalf("Run error = %v; want ErrSignatureRejected", err)
	}
}

func TestRun_ServerUnreachable_ReportsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	_, err := newSmoke(t, url, &fakeStore{ts: testTS}).Run(context.Background(), testRepo)
	if !errors.Is(err, smoke.ErrUnreachable) {
		t.Fatalf("Run error = %v; want ErrUnreachable", err)
	}
}
