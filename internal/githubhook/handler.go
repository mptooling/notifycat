package githubhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// EventSink receives a parsed Payload. It is the seam between the HTTP layer
// and the pullrequest dispatcher; defining it here keeps githubhook unaware
// of any downstream package.
type EventSink func(ctx context.Context, p Payload) error

// NewHandler returns an http.Handler that parses the JSON body of an inbound
// GitHub webhook and forwards the parsed Payload to sink.
//
// The handler assumes the body has already been validated by
// SignatureMiddleware. It returns:
//
//   - 400 if the body is not valid JSON,
//   - 400 if the payload has no pull_request.number,
//   - 200 with body `"ok"` after the sink runs successfully,
//   - 500 if the sink returns an error.
//
// Response bodies are intentionally generic; details go to logs.
func NewHandler(sink EventSink) http.Handler {
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
		if err := sink(r.Context(), payload); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode("ok")
	})
}
