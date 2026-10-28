package infrastructure

import (
	"net/http"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

type listRepositoriesResponse struct {
	Repositories  []string `json:"repositories"`
	Organizations []string `json:"organizations"`
}

type reposHandler struct {
	subscriptions domain.Subscriptions
}

func (handler *reposHandler) list(w http.ResponseWriter, request *http.Request) {
	if _, ok := subscriberFrom(request); !ok {
		writeUnauthorized(w)
		return
	}
	available := handler.subscriptions.Available()
	httpx.WriteJSON(w, http.StatusOK, listRepositoriesResponse{
		Repositories:  append([]string{}, available.Repositories...),
		Organizations: append([]string{}, available.Organizations...),
	})
}
