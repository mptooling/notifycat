package slackhook

import (
	"context"
	"io"
	"log/slog"
	"net/http"
)

// InteractionSink receives a parsed Interaction. It is the seam between the
// HTTP layer and whatever acts on the interaction (the click handler, added in
// a later issue); defining it here keeps slackhook unaware of any downstream
// package. A nil sink is allowed — the foundation endpoint verifies, parses,
// and logs without yet routing anywhere.
type InteractionSink func(ctx context.Context, interaction Interaction) error

// NewHandler returns an http.Handler that parses a Slack interaction body and
// forwards it to sink. It assumes the body has already been verified by
// SignatureMiddleware.
//
// Slack retries any delivery that does not return 2xx within ~3 seconds, so the
// handler always responds 200 once the signature is trusted: a malformed or
// unactionable payload is logged and ignored rather than retried. A sink error
// is logged but does not change the response — at-least-once retries of inbound
// clicks would do more harm than the dropped update.
func NewHandler(sink InteractionSink, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error("slack interaction: read body", slog.Any("err", err))
			w.WriteHeader(http.StatusOK)
			return
		}
		interaction, err := ParseInteraction(body)
		if err != nil {
			logger.Warn("ignored slack interaction", slog.String("reason", "unparseable_payload"), slog.Any("err", err))
			w.WriteHeader(http.StatusOK)
			return
		}

		logger.Info("slack interaction received",
			slog.String("type", interaction.Type),
			slog.String("action_id", firstActionID(interaction)),
			slog.String("user", interaction.User.ID),
		)

		if sink != nil {
			if err := sink(r.Context(), interaction); err != nil {
				logger.Error("slack interaction: sink failed",
					slog.String("type", interaction.Type),
					slog.Any("err", err))
			}
		}
		w.WriteHeader(http.StatusOK)
	})
}

// firstActionID returns the action_id of the first action, or "" when the
// interaction carries none. Logged for observability of which button fired.
func firstActionID(interaction Interaction) string {
	if len(interaction.Actions) == 0 {
		return ""
	}
	return interaction.Actions[0].ActionID
}
