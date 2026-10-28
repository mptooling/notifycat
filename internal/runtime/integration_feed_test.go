package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"gorm.io/gorm"

	accessdomain "github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

const (
	feedRepository = "acme/api"
	openedHeadSHA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pushedHeadSHA  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (locked *lockedBuffer) Write(data []byte) (int, error) {
	locked.mu.Lock()
	defer locked.mu.Unlock()
	return locked.buffer.Write(data)
}

func (locked *lockedBuffer) String() string {
	locked.mu.Lock()
	defer locked.mu.Unlock()
	return locked.buffer.String()
}

type feedPullRequest struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	HeadSHA    string `json:"head_sha"`
}

type feedSubscription struct {
	ID         int64  `json:"id"`
	Repository string `json:"repository"`
}

func feedConfig(t *testing.T, agentAPIEnabled bool) config.Config {
	t.Helper()

	cfg := agentAPIConfig(t)
	cfg.AgentAPIEnabled = agentAPIEnabled
	cfg.SlackBaseURL = newSlackFake(t).URL
	cfg.Mappings = seedsToMappings(t, []mappingSeed{{repository: feedRepository, channel: "C123ABCDE"}})
	primeLock(t, cfg.ConfigFile, routingapp.NewProvider(routingdomain.Defaults{}, cfg.Mappings, cfg.Digest))
	return cfg
}

func pullRequestEvent(action, headSHA string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"pull_request": {
			"number": 42, "title": "Retry failed deliveries", "body": "Adds a retry loop.",
			"html_url": "https://github.com/acme/api/pull/42",
			"user": {"login": "bob", "id": 1001}, "draft": false, "merged": false,
			"created_at": "2026-10-28T09:00:00Z",
			"head": {"sha": %q, "ref": "feature/retries"},
			"base": {"ref": "main"}
		}
	}`, action, feedRepository, headSHA)
}

func deliverPullRequestEvent(t *testing.T, public *httptest.Server, cfg config.Config, payload string) int {
	t.Helper()

	status, _ := request(t, public, http.MethodPost, "/webhook/github", payload, map[string]string{
		"Content-Type":        "application/json",
		"X-GitHub-Event":      "pull_request",
		"X-Hub-Signature-256": githubSignature(cfg.GitHubWebhookSecret.Reveal(), payload),
	})
	return status
}

func listFeed(t *testing.T, public *httptest.Server, query string, tokenAuth map[string]string) []feedPullRequest {
	t.Helper()

	status, body := request(t, public, http.MethodGet, "/api/v1/subs/prs"+query, "", tokenAuth)
	require.Equal(t, http.StatusOK, status)
	var listed struct {
		PullRequests []feedPullRequest `json:"pull_requests"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &listed))
	return listed.PullRequests
}

func redeemAgentToken(t *testing.T, public, admin *httptest.Server, adminAuth map[string]string) string {
	t.Helper()

	createStatus, createBody := request(t, admin, http.MethodPost, "/admin/v1/invites", `{"user":"alice"}`, adminAuth)
	require.Equal(t, http.StatusCreated, createStatus)
	var invite struct {
		Invite string `json:"invite"`
	}
	require.NoError(t, json.Unmarshal([]byte(createBody), &invite))

	redeemStatus, redeemBody := request(t, public, http.MethodPost, "/api/v1/invites/redeem", "",
		map[string]string{"Authorization": "Invite " + invite.Invite})
	require.Equal(t, http.StatusCreated, redeemStatus)
	var redeemed struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal([]byte(redeemBody), &redeemed))
	return redeemed.Token
}

func TestIntegration_AgentFeedFlow(t *testing.T) {
	cfg := feedConfig(t, true)
	logged := &lockedBuffer{}
	debugLogger := slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	public, adminServer := buildAgentAPIApp(t, cfg, fx.Replace(debugLogger))
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	adminAuth := map[string]string{"Authorization": "Bearer " + testAdminKey}

	token := redeemAgentToken(t, public, admin, adminAuth)
	credential, err := accessdomain.ParseCredential(token, accessdomain.TokenPrefix)
	require.NoError(t, err)
	tokenAuth := map[string]string{"Authorization": "Bearer " + token}

	subscribeStatus, subscribeBody := request(t, public, http.MethodPost, "/api/v1/subs", `{"repositories":["Acme/API"]}`, tokenAuth)
	require.Equal(t, http.StatusOK, subscribeStatus)
	var subscribed struct {
		Subscriptions []feedSubscription `json:"subscriptions"`
	}
	require.NoError(t, json.Unmarshal([]byte(subscribeBody), &subscribed))
	require.Len(t, subscribed.Subscriptions, 1)
	require.Equal(t, feedRepository, subscribed.Subscriptions[0].Repository)
	sinceBeforeOpenedQuery := "?since=" + time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)

	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA)))
	opened := listFeed(t, public, "", tokenAuth)
	require.Equal(t, []feedPullRequest{{Repository: feedRepository, Number: 42, HeadSHA: openedHeadSHA}}, opened)
	require.Empty(t, listFeed(t, public, "", tokenAuth), "a pull request is delivered once per token")

	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("synchronize", pushedHeadSHA)))
	require.Empty(t, listFeed(t, public, "", tokenAuth), "a push does not deliver the pull request again")
	pushed := listFeed(t, public, sinceBeforeOpenedQuery, tokenAuth)
	require.Equal(t, []feedPullRequest{{Repository: feedRepository, Number: 42, HeadSHA: pushedHeadSHA}}, pushed)
	require.Empty(t, listFeed(t, public, "", tokenAuth), "a since read records no delivery")

	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("closed", pushedHeadSHA)))
	require.Empty(t, listFeed(t, public, sinceBeforeOpenedQuery, tokenAuth), "a closed pull request leaves the feed")

	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("reopened", pushedHeadSHA)))
	reopened := listFeed(t, public, "", tokenAuth)
	require.Equal(t, []feedPullRequest{{Repository: feedRepository, Number: 42, HeadSHA: pushedHeadSHA}}, reopened)

	unsubscribePath := "/api/v1/subs/" + strconv.FormatInt(subscribed.Subscriptions[0].ID, 10)
	unsubscribeStatus, _ := request(t, public, http.MethodDelete, unsubscribePath, "", tokenAuth)
	require.Equal(t, http.StatusNoContent, unsubscribeStatus)
	revokeStatus, _ := request(t, admin, http.MethodDelete, "/admin/v1/tokens/"+credential.ID, "", adminAuth)
	require.Equal(t, http.StatusNoContent, revokeStatus)
	revokedStatus, _ := request(t, public, http.MethodGet, "/api/v1/subs/prs", "", tokenAuth)

	assert.Equal(t, http.StatusUnauthorized, revokedStatus)
	assert.Contains(t, logged.String(), "feed pull requests delivered", "the buffer captures the server logs")
	assert.NotContains(t, logged.String(), token)
	assert.NotContains(t, logged.String(), credential.Secret)
}

func TestIntegration_AgentAPIDisabled_NothingTracked(t *testing.T) {
	cfg := feedConfig(t, false)
	var db *gorm.DB
	public, _ := buildAgentAPIApp(t, cfg, fx.Populate(&db))

	webhookStatus := deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA))
	unknownToken := map[string]string{"Authorization": "Bearer nct_unknown"}
	subscriptionsStatus, _ := request(t, public, http.MethodGet, "/api/v1/subs", "", unknownToken)
	pullRequestsStatus, _ := request(t, public, http.MethodGet, "/api/v1/subs/prs", "", unknownToken)

	var trackedPullRequests int64
	require.NoError(t, db.Table("feed_pull_requests").Count(&trackedPullRequests).Error)
	assert.Equal(t, http.StatusOK, webhookStatus)
	assert.Equal(t, http.StatusNotFound, subscriptionsStatus)
	assert.Equal(t, http.StatusNotFound, pullRequestsStatus)
	assert.Zero(t, trackedPullRequests)
}

func TestIntegration_SubscribeToAnOrgTierIsUnknownRepository(t *testing.T) {
	cfg := feedConfig(t, true)
	cfg.Mappings = seedsToMappings(t, []mappingSeed{{repository: "acme/*", channel: "C123ABCDE"}})
	primeLock(t, cfg.ConfigFile, routingapp.NewProvider(routingdomain.Defaults{}, cfg.Mappings, cfg.Digest))
	public, adminServer := buildAgentAPIApp(t, cfg)
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	token := redeemAgentToken(t, public, admin, map[string]string{"Authorization": "Bearer " + testAdminKey})

	status, body := request(t, public, http.MethodPost, "/api/v1/subs", `{"repositories":["acme/*"]}`,
		map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusBadRequest, status)
	assert.JSONEq(t, `{"error":"unknown_repository","repositories":["acme/*"]}`, body)
}

func seedFeedPullRequests(t *testing.T, cfg config.Config, repositories ...string) {
	t.Helper()

	db, err := persistence.Open(cfg.DatabaseURL)
	require.NoError(t, err)
	defer func() {
		if sqlDB, closeErr := persistence.SQLDB(db); closeErr == nil {
			_ = sqlDB.Close()
		}
	}()
	require.NoError(t, persistence.MigrateUp(context.Background(), db))
	pullRequests := persistence.NewFeedPullRequests(db)
	for index, repository := range repositories {
		require.NoError(t, pullRequests.Upsert(context.Background(), persistence.FeedPullRequest{
			Repository: repository, Number: index + 1, HeadSHA: openedHeadSHA, UpdatedAt: time.Now(),
		}))
	}
}

func storedFeedRepositories(t *testing.T, db *gorm.DB) []string {
	t.Helper()

	var repositories []string
	require.NoError(t, db.Table("feed_pull_requests").Order("repository").Pluck("repository", &repositories).Error)
	return repositories
}

func TestIntegration_StartupWithAgentAPIOffRemovesFeedPullRequests(t *testing.T) {
	cfg := feedConfig(t, false)
	seedFeedPullRequests(t, cfg, feedRepository)
	var db *gorm.DB

	buildAgentAPIApp(t, cfg, fx.Populate(&db))

	assert.Empty(t, storedFeedRepositories(t, db), "nothing records a close while the API is off")
}

func TestIntegration_StartupRemovesFeedPullRequestsOfUnmappedRepositories(t *testing.T) {
	cfg := feedConfig(t, true)
	seedFeedPullRequests(t, cfg, feedRepository, "acme/gone")
	var db *gorm.DB

	buildAgentAPIApp(t, cfg, fx.Populate(&db))

	assert.Equal(t, []string{feedRepository}, storedFeedRepositories(t, db))
}
