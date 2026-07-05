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

// BitbucketSignatureMiddleware returns an HTTP middleware that rejects any
// request whose body exceeds MaxBodyBytes (413) or whose X-Hub-Signature header
// does not match the HMAC of the body (401), and passes a fresh body reader to
// next so downstream handlers read the verified body. Bitbucket sends no
// signature header when no secret is configured, so a missing header is rejected
// 401 — unsigned deliveries are never trusted.
func BitbucketSignatureMiddleware(verifier security.SignatureVerifier) func(http.Handler) http.Handler {
	return httpx.Signature(MaxBodyBytes, func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		signature := r.Header.Get(security.SignatureHeaderBitbucket)
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

// NewBitbucketHandler returns an http.Handler that parses the JSON body of an
// inbound Bitbucket webhook, maps it to a kernel.Event, and dispatches it. It
// assumes the body has already been validated by BitbucketSignatureMiddleware.
// It returns 400 if the body is not valid JSON or has no pullrequest.id, 200
// with body "ok" after a successful dispatch, and 500 if dispatch returns an
// error.
func NewBitbucketHandler(dispatcher domain.EventDispatcher) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		payload, err := parseBitbucketPayload(body)
		if err != nil {
			if errors.Is(err, ErrMissingPRNumber) {
				http.Error(w, "missing pr number", http.StatusBadRequest)
				return
			}
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		eventKey := r.Header.Get(bbEventKeyHeader)
		if err := dispatcher.Dispatch(r.Context(), toBitbucketEvent(eventKey, payload)); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode("ok")
	})
}

// bitbucketPayload is the parsed view of an inbound Bitbucket webhook body,
// holding only the fields the notifier uses.
type bitbucketPayload struct {
	Repository string
	Actor      bitbucketActor

	PullRequest bitbucketPullRequest
}

// bitbucketActor identifies the actor that fired the webhook.
type bitbucketActor struct {
	DisplayName string
	Type        string
}

// bitbucketPullRequest holds the PR fields extracted from the payload.
type bitbucketPullRequest struct {
	ID          int
	Title       string
	URL         string
	Author      string
	State       string
	Draft       bool
	Description string
	CreatedAt   time.Time
}

// rawBitbucketPayload mirrors only the JSON fields we read. Unknown fields are
// ignored.
type rawBitbucketPayload struct {
	Actor struct {
		Type        string `json:"type"`
		DisplayName string `json:"display_name"`
	} `json:"actor"`
	PullRequest struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       string `json:"state"`
		Draft       bool   `json:"draft"`
		CreatedOn   string `json:"created_on"`
		Links       struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
		Author struct {
			DisplayName string `json:"display_name"`
			Type        string `json:"type"`
		} `json:"author"`
	} `json:"pullrequest"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// parseBitbucketPayload decodes a raw Bitbucket webhook body into a
// bitbucketPayload. It validates only what the dispatcher needs (a PR id);
// everything else is best-effort, and a malformed created_on falls back to the
// zero time rather than failing.
func parseBitbucketPayload(body []byte) (bitbucketPayload, error) {
	var raw rawBitbucketPayload
	if err := json.Unmarshal(body, &raw); err != nil {
		return bitbucketPayload{}, fmt.Errorf("notification: decode bitbucket payload: %w", err)
	}
	if raw.PullRequest.ID == 0 {
		return bitbucketPayload{}, ErrMissingPRNumber
	}

	var createdAt time.Time
	if s := raw.PullRequest.CreatedOn; s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			createdAt = t
		}
	}

	return bitbucketPayload{
		Repository: raw.Repository.FullName,
		Actor:      bitbucketActor{DisplayName: raw.Actor.DisplayName, Type: raw.Actor.Type},
		PullRequest: bitbucketPullRequest{
			ID:          raw.PullRequest.ID,
			Title:       raw.PullRequest.Title,
			URL:         raw.PullRequest.Links.HTML.Href,
			Author:      raw.PullRequest.Author.DisplayName,
			State:       raw.PullRequest.State,
			Draft:       raw.PullRequest.Draft,
			Description: raw.PullRequest.Description,
			CreatedAt:   createdAt,
		},
	}, nil
}

// Bitbucket webhook vocabulary. Confined to this adapter — the kind-mapper is
// the only code that reads these event keys and the actor-type token; no other
// package speaks Bitbucket verbs.
const (
	bbEventKeyHeader = "X-Event-Key"

	bbEventCreated        = "pullrequest:created"
	bbEventUpdated        = "pullrequest:updated"
	bbEventFulfilled      = "pullrequest:fulfilled"
	bbEventRejected       = "pullrequest:rejected"
	bbEventApproved       = "pullrequest:approved"
	bbEventChangesRequest = "pullrequest:changes_request_created"
	bbEventCommentCreated = "pullrequest:comment_created"

	bbStateOpen   = "OPEN"
	bbStateMerged = "MERGED"

	bbActorTypeUser = "user"
)

// mapBitbucketKind classifies a parsed Bitbucket payload into a neutral
// kernel.EventKind, keyed on the X-Event-Key request header (not a body field).
// It owns every Bitbucket-vocabulary decision and all draft gating: a draft
// create is gated to KindUnknown (mirroring GitHub), an update splits into
// draft/ready by the draft flag and is ignored unless the PR is OPEN, and
// anything unmapped is KindUnknown so the dispatcher debug-logs no_handler.
func mapBitbucketKind(eventKey string, p bitbucketPayload) kernel.EventKind {
	switch eventKey {
	case bbEventCreated:
		if p.PullRequest.Draft {
			return kernel.KindUnknown
		}
		return kernel.KindOpened
	case bbEventUpdated:
		if p.PullRequest.Draft {
			return kernel.KindConvertedToDraft
		}
		if p.PullRequest.State == bbStateOpen {
			return kernel.KindReadyForReview
		}
		return kernel.KindUnknown
	case bbEventFulfilled:
		return kernel.KindMerged
	case bbEventRejected:
		return kernel.KindClosed
	case bbEventApproved:
		return kernel.KindApproved
	case bbEventChangesRequest:
		return kernel.KindChangesRequested
	case bbEventCommentCreated:
		return kernel.KindCommented
	}
	return kernel.KindUnknown
}

// toBitbucketEvent maps a parsed bitbucketPayload to the neutral kernel event the
// dispatcher consumes, resolving the Bitbucket actor type to Sender.IsBot (any
// type other than "user" — e.g. "team", "app_user" — is a bot).
func toBitbucketEvent(eventKey string, p bitbucketPayload) kernel.Event {
	return kernel.Event{
		Provider:   kernel.ProviderBitbucket,
		Kind:       mapBitbucketKind(eventKey, p),
		Repository: p.Repository,
		PR: kernel.PR{
			Number:    p.PullRequest.ID,
			Title:     p.PullRequest.Title,
			URL:       p.PullRequest.URL,
			Author:    p.PullRequest.Author,
			Merged:    p.PullRequest.State == bbStateMerged,
			Draft:     p.PullRequest.Draft,
			Body:      p.PullRequest.Description,
			CreatedAt: p.PullRequest.CreatedAt,
		},
		Sender: kernel.Sender{Login: p.Actor.DisplayName, IsBot: p.Actor.Type != bbActorTypeUser},
	}
}
