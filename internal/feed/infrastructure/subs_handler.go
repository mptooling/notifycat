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
	Type         string   `json:"type"`
}

type changeSubscriptionsRequest struct {
	Repositories json.RawMessage `json:"repositories"`
	Type         string          `json:"type"`
}

type subscriptionResponse struct {
	ID         int64  `json:"id"`
	Repository string `json:"repository"`
	Type       string `json:"type"`
	CreatedAt  string `json:"created_at"`
}

type listSubscriptionsResponse struct {
	Subscriptions []subscriptionResponse `json:"subscriptions"`
}

type repositoriesErrorResponse struct {
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
	body, ok := decodeSubscriptionsBody[addSubscriptionsRequest](w, request)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidBody))
		return
	}
	subscriptions, err := handler.subscriptions.Add(request.Context(), subscriber, domain.SubscriptionRequest{
		Repositories: body.Repositories,
		Type:         domain.SubscriptionType(body.Type),
	})
	if handler.writeSubscriptionsError(w, request, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toListSubscriptionsResponse(subscriptions))
}

func (handler *subsHandler) change(w http.ResponseWriter, request *http.Request) {
	subscriber, ok := subscriberFrom(request)
	if !ok {
		writeUnauthorized(w)
		return
	}
	body, ok := decodeSubscriptionsBody[changeSubscriptionsRequest](w, request)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidBody))
		return
	}
	repositories, ok := decodeChangeRepositories(body.Repositories)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidBody))
		return
	}
	change := domain.TypeChange{Repositories: repositories, Type: domain.SubscriptionType(body.Type)}
	subscriptions, err := handler.subscriptions.ChangeType(request.Context(), subscriber, change)
	if handler.writeSubscriptionsError(w, request, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toListSubscriptionsResponse(subscriptions))
}

func (handler *subsHandler) writeSubscriptionsError(w http.ResponseWriter, request *http.Request, err error) bool {
	var unknown *domain.UnknownRepositoriesError
	var notSubscribed *domain.NotSubscribedError
	switch {
	case err == nil:
		return false
	case errors.Is(err, domain.ErrNoRepositories):
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidBody))
	case errors.As(err, &unknown):
		httpx.WriteJSON(w, http.StatusBadRequest, repositoriesErrorResponse{
			Error:        string(domain.ErrorCodeUnknownRepository),
			Repositories: unknown.Repositories,
		})
	case errors.As(err, &notSubscribed):
		httpx.WriteJSON(w, http.StatusBadRequest, repositoriesErrorResponse{
			Error:        string(domain.ErrorCodeNotSubscribed),
			Repositories: notSubscribed.Repositories,
		})
	case errors.Is(err, domain.ErrInvalidType):
		httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidType))
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

func decodeChangeRepositories(raw json.RawMessage) ([]string, bool) {
	if raw == nil {
		return nil, true
	}
	var repositories []string
	if err := json.Unmarshal(raw, &repositories); err != nil || repositories == nil {
		return nil, false
	}
	return repositories, true
}

func decodeSubscriptionsBody[Body any](w http.ResponseWriter, request *http.Request) (Body, bool) {
	var body Body
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
			Type:       string(subscription.Type),
			CreatedAt:  formatTime(subscription.CreatedAt),
		})
	}
	return response
}
