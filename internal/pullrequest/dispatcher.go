package pullrequest

import (
	"context"
	"fmt"
)

// Dispatcher routes an Event to the first registered EventHandler whose
// Applicable returns true. Handlers are checked in the order they were
// registered. If no handler matches, Dispatch returns nil (no error).
type Dispatcher struct {
	handlers []EventHandler
}

// NewDispatcher builds a Dispatcher with the given handlers. Registration
// order is preserved.
func NewDispatcher(handlers ...EventHandler) *Dispatcher {
	return &Dispatcher{handlers: handlers}
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
	return nil
}
