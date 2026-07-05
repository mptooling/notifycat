package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mptooling/notifycat/internal/kernel"
	"github.com/mptooling/notifycat/internal/notification/domain"
)

// Dispatcher routes an event to the first registered handler whose Applicable
// returns true. Handlers are checked in registration order. If none matches,
// Dispatch logs a Debug line and returns nil (no error) so the HTTP layer still
// responds 200 OK.
type Dispatcher struct {
	handlers []domain.Handler
	logger   *slog.Logger
}

// NewDispatcher builds a Dispatcher with the given handlers. Registration order
// is preserved. The logger receives one Debug record per event with no
// applicable handler.
func NewDispatcher(logger *slog.Logger, handlers []domain.Handler) *Dispatcher {
	return &Dispatcher{handlers: handlers, logger: logger}
}

// Dispatch finds the first applicable handler and runs it. Errors from the
// chosen handler are wrapped and returned.
func (d *Dispatcher) Dispatch(ctx context.Context, event kernel.Event) error {
	for _, handler := range d.handlers {
		if !handler.Applicable(event) {
			continue
		}
		if err := handler.Handle(ctx, event); err != nil {
			return fmt.Errorf("notification: dispatch: %w", err)
		}
		return nil
	}
	d.logger.Debug("ignored webhook event",
		slog.String("reason", domain.ReasonNoHandler),
		slog.String("handler", ""),
		slog.String("provider", event.Provider.String()),
		slog.String("kind", event.Kind.String()),
		slog.String("repository", event.Repository),
		slog.Int("pr", event.PR.Number),
	)
	return nil
}

var _ domain.EventDispatcher = (*Dispatcher)(nil)
