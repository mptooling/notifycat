package infrastructure

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

type addSubscriptionsRequest struct {
	Repositories []string `json:"repositories"`
}

type subscriptionResponse struct {
	ID         int64  `json:"id"`
	Repository string `json:"repository"`
	CreatedAt  string `json:"created_at"`
}

type listSubscriptionsResponse struct {
	Subscriptions []subscriptionResponse `json:"subscriptions"`
}

type unknownRepositoryResponse struct {
	Error        string   `json:"error"`
	Repositories []string `json:"repositories"`
}

type subsHandler struct {
	subscriptions domain.Subscriptions
	logger        *slog.Logger
}

func (handler *subsHandler) list(w http.ResponseWriter, request *http.Request) {
	subscriber, ok := subscriberFrom(request)
	if !ok {
		writeUnauthorized(w)
		return
	}
	subscriptions, err := handler.subscriptions.List(request.Context(), subscriber)
	if err != nil {
		writeInternal(w, request, handler.logger, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toListSubscriptionsResponse(subscriptions))
}

func (handler *subsHandler) add(w http.ResponseWriter, request *http.Request) {
	subscriber, ok := subscriberFrom(request)
	if !ok {
		writeUnauthorized(w)
		return
	}
	body, ok := decodeAddSubscriptions(w, request)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidBody))
		return
	}
	subscriptions, err := handler.subscriptions.Add(request.Context(), subscriber, body.Repositories)
	if handler.writeAddError(w, request, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toListSubscriptionsResponse(subscriptions))
}

func (handler *subsHandler) writeAddError(w http.ResponseWriter, request *http.Request, err error) bool {
	var unknown *domain.UnknownRepositoriesError
	switch {
	case err == nil:
		return false
	case errors.Is(err, domain.ErrNoRepositories):
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidBody))
	case errors.As(err, &unknown):
		httpx.WriteJSON(w, http.StatusBadRequest, unknownRepositoryResponse{
			Error:        string(domain.ErrorCodeUnknownRepository),
			Repositories: unknown.Repositories,
		})
	case errors.Is(err, domain.ErrTooManySubscriptions):
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeTooManySubscriptions))
	default:
		writeInternal(w, request, handler.logger, err)
	}
	return true
}

func (handler *subsHandler) remove(w http.ResponseWriter, request *http.Request) {
	subscriber, ok := subscriberFrom(request)
	if !ok {
		writeUnauthorized(w)
		return
	}
	id, err := strconv.ParseInt(request.PathValue(domain.SubscriptionIDPathValue), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusNotFound, string(domain.ErrorCodeNotFound))
		return
	}
	err = handler.subscriptions.Remove(request.Context(), subscriber, id)
	if errors.Is(err, domain.ErrSubscriptionNotFound) {
		httpx.WriteError(w, http.StatusNotFound, string(domain.ErrorCodeNotFound))
		return
	}
	if err != nil {
		writeInternal(w, request, handler.logger, err)
		return
	}
	httpx.WriteNoContent(w)
}

func decodeAddSubscriptions(w http.ResponseWriter, request *http.Request) (addSubscriptionsRequest, bool) {
	var body addSubscriptionsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, domain.MaxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return body, false
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return body, false
	}
	return body, true
}

func toListSubscriptionsResponse(subscriptions []domain.Subscription) listSubscriptionsResponse {
	response := listSubscriptionsResponse{Subscriptions: make([]subscriptionResponse, 0, len(subscriptions))}
	for _, subscription := range subscriptions {
		response.Subscriptions = append(response.Subscriptions, subscriptionResponse{
			ID:         subscription.ID,
			Repository: subscription.Repository,
			CreatedAt:  formatTime(subscription.CreatedAt),
		})
	}
	return response
}
