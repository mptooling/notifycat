package infrastructure

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
	"github.com/mptooling/notifycat/internal/platform/security"
)

// MaxBodyBytes caps the size of an accepted webhook body. GitHub limits its own
// webhook payloads to ~25 MiB but our handler needs nowhere near that — a
// generous 1 MiB protects against memory exhaustion.
const MaxBodyBytes int64 = 1 << 20 // 1 MiB

// SignatureMiddleware returns an HTTP middleware that rejects any request whose
// body exceeds MaxBodyBytes (413) or whose X-Hub-Signature-256 header does not
// match the HMAC of the body (401), and passes a fresh body reader to next so
// downstream handlers read the verified body.
func SignatureMiddleware(verifier security.SignatureVerifier) func(http.Handler) http.Handler {
	return httpx.Signature(MaxBodyBytes, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		signature := r.Header.Get(security.SignatureHeader)
		if signature == "" {
			http.Error(w, "missing signature", http.StatusUnauthorized)
			return false
		}
		if err := verifier.Verify(body, signature); err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return false
		}
		return true
	})
}

// NewGitHubHandler returns an http.Handler that parses the JSON body of an
// inbound GitHub webhook, maps it to a kernel.Event, and dispatches it. It
// assumes the body has already been validated by SignatureMiddleware. It returns
// 400 if the body is not valid JSON or has no pull_request.number, 200 with body
// "ok" after a successful dispatch, and 500 if dispatch returns an error.
func NewGitHubHandler(dispatcher domain.EventDispatcher) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		payload, err := ParsePayload(body)
		if err != nil {
			if errors.Is(err, ErrMissingPRNumber) {
				http.Error(w, "missing pr number", http.StatusBadRequest)
				return
			}
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		payload.Event = r.Header.Get("X-GitHub-Event")
		if err := dispatcher.Dispatch(r.Context(), toEvent(payload)); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode("ok")
	})
}

// Payload is the parsed view of an inbound GitHub webhook body, holding only the
// fields the notifier uses.
type Payload struct {
	Event      string
	Action     string
	Repository string

	PullRequest PullRequest

	// Review is non-nil only for pull_request_review events.
	Review *Review

	// PRComment is true for issue_comment events fired on a pull request.
	PRComment bool

	// Sender is the actor who fired the event.
	Sender Sender
}

// Sender identifies the actor that fired the webhook.
type Sender struct {
	Login string
	Type  string
}

// PullRequest holds the PR fields extracted from the payload.
type PullRequest struct {
	Number    int
	Title     string
	URL       string
	Author    string
	Merged    bool
	Draft     bool
	Body      string
	CreatedAt time.Time
}

// Review carries the review state (approved | commented | changes_requested).
type Review struct {
	State string
}

// ErrMissingPRNumber is returned when the payload lacks a pull request number.
var ErrMissingPRNumber = errors.New("notification: missing pull_request.number")

// rawPayload mirrors only the JSON fields we read. Unknown fields are ignored.
type rawPayload struct {
	Action     string `json:"action"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		HTMLURL   string `json:"html_url"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Merged bool `json:"merged"`
		Draft  bool `json:"draft"`
	} `json:"pull_request"`
	Review *struct {
		State string `json:"state"`
	} `json:"review"`
	Issue struct {
		Number      int `json:"number"`
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	} `json:"issue"`
	Sender struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"sender"`
}

// ParsePayload decodes a raw GitHub webhook body into a Payload. It validates
// only what the dispatcher needs (a PR number); everything else is best-effort.
func ParsePayload(body []byte) (Payload, error) {
	var raw rawPayload
	if err := json.Unmarshal(body, &raw); err != nil {
		return Payload{}, fmt.Errorf("notification: decode payload: %w", err)
	}
	// issue_comment events carry the PR number under issue.number; the presence
	// of issue.pull_request marks the comment as a PR conversation comment. A
	// plain-issue comment parses with PRComment=false and PR number 0 so the
	// dispatcher ignores it as no_handler rather than 400-ing.
	number := raw.PullRequest.Number
	prComment := false
	if number == 0 && raw.Issue.PullRequest != nil {
		number = raw.Issue.Number
		prComment = true
	}
	if number == 0 && raw.Issue.Number == 0 {
		return Payload{}, ErrMissingPRNumber
	}

	var createdAt time.Time
	if s := raw.PullRequest.CreatedAt; s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			createdAt = t
		}
	}

	p := Payload{
		Action:     raw.Action,
		Repository: raw.Repository.FullName,
		PullRequest: PullRequest{
			Number:    number,
			Title:     raw.PullRequest.Title,
			URL:       raw.PullRequest.HTMLURL,
			Author:    raw.PullRequest.User.Login,
			Merged:    raw.PullRequest.Merged,
			Draft:     raw.PullRequest.Draft,
			Body:      raw.PullRequest.Body,
			CreatedAt: createdAt,
		},
		PRComment: prComment,
		Sender:    Sender{Login: raw.Sender.Login, Type: raw.Sender.Type},
	}
	if raw.Review != nil {
		p.Review = &Review{State: raw.Review.State}
	}
	return p, nil
}

// toEvent maps a parsed Payload to the kernel event the dispatcher consumes.
func toEvent(p Payload) kernel.Event {
	event := kernel.Event{
		GitHubEvent: kernel.GitHubEventType(p.Event),
		Action:      kernel.Action(p.Action),
		Repository:  p.Repository,
		PR: kernel.PR{
			Number:    p.PullRequest.Number,
			Title:     p.PullRequest.Title,
			URL:       p.PullRequest.URL,
			Author:    p.PullRequest.Author,
			Merged:    p.PullRequest.Merged,
			Draft:     p.PullRequest.Draft,
			Body:      p.PullRequest.Body,
			CreatedAt: p.PullRequest.CreatedAt,
		},
		PRComment: p.PRComment,
		Sender:    kernel.Sender{Login: p.Sender.Login, Type: p.Sender.Type},
	}
	if p.Review != nil {
		event.Review = &kernel.Review{State: kernel.ReviewState(p.Review.State)}
	}
	return event
}
