package infrastructure

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

type pullRequestResponse struct {
	Repository   string  `json:"repository"`
	Number       int     `json:"number"`
	Title        string  `json:"title"`
	Description  string  `json:"description"`
	URL          string  `json:"url"`
	AuthorID     string  `json:"author_id"`
	SourceBranch string  `json:"source_branch"`
	TargetBranch string  `json:"target_branch"`
	HeadSHA      string  `json:"head_sha"`
	CreatedAt    *string `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type listPullRequestsResponse struct {
	PullRequests []pullRequestResponse `json:"pull_requests"`
}

type prsHandler struct {
	pullRequests domain.PullRequests
	logger       *slog.Logger
}

func (handler *prsHandler) list(w http.ResponseWriter, request *http.Request) {
	subscriber, ok := subscriberFrom(request)
	if !ok {
		writeUnauthorized(w)
		return
	}
	var (
		pullRequests []domain.PullRequest
		err          error
	)
	query := request.URL.Query()
	if query.Has(domain.SinceQueryParameter) {
		since, parseErr := time.Parse(time.RFC3339, query.Get(domain.SinceQueryParameter))
		if parseErr != nil {
			httpx.WriteError(w, http.StatusBadRequest, string(domain.ErrorCodeInvalidSince))
			return
		}
		pullRequests, err = handler.pullRequests.Since(request.Context(), subscriber, since)
	} else {
		pullRequests, err = handler.pullRequests.Next(request.Context(), subscriber)
	}
	if err != nil {
		writeInternal(w, request, handler.logger, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toListPullRequestsResponse(pullRequests))
}

func toListPullRequestsResponse(pullRequests []domain.PullRequest) listPullRequestsResponse {
	response := listPullRequestsResponse{PullRequests: make([]pullRequestResponse, 0, len(pullRequests))}
	for _, pullRequest := range pullRequests {
		item := pullRequestResponse{
			Repository:   pullRequest.Repository,
			Number:       pullRequest.Number,
			Title:        pullRequest.Title,
			Description:  pullRequest.Description,
			URL:          pullRequest.URL,
			AuthorID:     pullRequest.AuthorID,
			SourceBranch: pullRequest.SourceBranch,
			TargetBranch: pullRequest.TargetBranch,
			HeadSHA:      pullRequest.HeadSHA,
			UpdatedAt:    formatTime(pullRequest.UpdatedAt),
		}
		if !pullRequest.CreatedAt.IsZero() {
			createdAt := formatTime(pullRequest.CreatedAt)
			item.CreatedAt = &createdAt
		}
		response.PullRequests = append(response.PullRequests, item)
	}
	return response
}
