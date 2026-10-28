package infrastructure

import (
	"log/slog"
	"net/http"
	"time"

	accessdomain "github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

// NewRoutes returns the agent feed routes; the caller guards them with the bearer middleware.
func NewRoutes(subscriptions domain.Subscriptions, pullRequests domain.PullRequests, logger *slog.Logger) []httpx.Route {
	subscriptionsHandler := &subsHandler{subscriptions: subscriptions, logger: logger}
	pullRequestsHandler := &prsHandler{pullRequests: pullRequests, logger: logger}
	return []httpx.Route{
		{Pattern: domain.ListSubscriptionsPattern, Handler: http.HandlerFunc(subscriptionsHandler.list)},
		{Pattern: domain.AddSubscriptionsPattern, Handler: http.HandlerFunc(subscriptionsHandler.add)},
		{Pattern: domain.RemoveSubscriptionPattern, Handler: http.HandlerFunc(subscriptionsHandler.remove)},
		{Pattern: domain.PullRequestsPattern, Handler: http.HandlerFunc(pullRequestsHandler.list)},
	}
}

func subscriberFrom(request *http.Request) (domain.Subscriber, bool) {
	principal, ok := accessdomain.PrincipalFrom(request.Context())
	if !ok {
		return domain.Subscriber{}, false
	}
	return domain.Subscriber{TokenID: principal.TokenID, User: principal.User}, true
}

func writeUnauthorized(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusUnauthorized, string(domain.ErrorCodeUnauthorized))
}

func writeInternal(w http.ResponseWriter, request *http.Request, logger *slog.Logger, err error) {
	logger.ErrorContext(request.Context(), "agent api request failed", "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, string(domain.ErrorCodeInternal))
}

func formatTime(moment time.Time) string {
	return moment.UTC().Format(time.RFC3339)
}
