package pullrequest

import (
	"context"
	"fmt"
	"log/slog"
)

// Dispatcher routes an Event to the first registered EventHandler whose
// Applicable returns true. Handlers are checked in the order they were
// registered. If no handler matches, Dispatch logs a Debug line and returns
// nil (no error) so the HTTP layer still responds 200 OK.
type Dispatcher struct {
	handlers []EventHandler
	logger   *slog.Logger
}

// NewDispatcher builds a Dispatcher with the given handlers. Registration
// order is preserved. The logger receives one Debug record per event with no
// applicable handler — operators enable LOG_LEVEL=debug to triage silent
// 200 OK deliveries.
func NewDispatcher(logger *slog.Logger, handlers ...EventHandler) *Dispatcher {
	return &Dispatcher{handlers: handlers, logger: logger}
}

// Dispatch finds the first applicable handler and runs it. Errors from the
// chosen handler are wrapped and returned.
func (d *Dispatcher) Dispatch(ctx context.Context, e Event) error {
	for _, h := range d.handlers {
		if !h.Applicable(e) {
			continue
		}
		if err := h.Handle(ctx, e); err != nil {
			return fmt.Errorf("pullrequest: dispatch: %w", err)
		}
		return nil
	}
	d.logger.Debug("ignored webhook event",
		slog.String("reason", "no_handler"),
		slog.String("handler", ""),
		slog.String("github_event", e.GitHubEvent),
		slog.String("action", e.Action),
		slog.String("repository", e.Repository),
		slog.Int("pr", e.PR.Number),
	)
	return nil
}
