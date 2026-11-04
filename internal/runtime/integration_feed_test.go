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
	feeddomain "github.com/mptooling/notifycat/internal/feed/domain"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingapp "github.com/mptooling/notifycat/internal/routing/application"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
	"github.com/mptooling/notifycat/internal/runtime"
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

type movingClock struct {
	start   time.Time
	started time.Time
}

func (clock *movingClock) Now() time.Time {
	return clock.start.Add(time.Since(clock.started)).UTC()
}

func weekdayClock() *movingClock {
	return &movingClock{start: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC), started: time.Now()}
}

func buildFeedApp(t *testing.T, cfg config.Config, clock feeddomain.Clock, options ...fx.Option) (*httptest.Server, *runtime.AdminServer) {
	t.Helper()

	feedClock := fx.Decorate(func(feeddomain.Clock) feeddomain.Clock { return clock })
	return buildAgentAPIApp(t, cfg, append([]fx.Option{feedClock}, options...)...)
}

func sinceQuery(clock feeddomain.Clock) string {
	return "?since=" + clock.Now().Truncate(time.Second).Format(time.RFC3339)
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
	return numberedPullRequestEvent(action, 42, headSHA)
}

func numberedPullRequestEvent(action string, number int, headSHA string) string {
	return fmt.Sprintf(`{
		"action": %q,
		"repository": {"full_name": %q},
		"pull_request": {
			"number": %d, "title": "Retry failed deliveries", "body": "Adds a retry loop.",
			"html_url": %q,
			"user": {"login": "bob", "id": 1001}, "draft": false, "merged": false,
			"created_at": "2026-10-28T09:00:00Z",
			"head": {"sha": %q, "ref": "feature/retries"},
			"base": {"ref": "main"}
		}
	}`, action, feedRepository, number, fmt.Sprintf("https://github.com/%s/pull/%d", feedRepository, number), headSHA)
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

func pullRequestNumbers(pullRequests []feedPullRequest) []int {
	numbers := make([]int, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		numbers = append(numbers, pullRequest.Number)
	}
	return numbers
}

func redeemAgentToken(t *testing.T, public, admin *httptest.Server, adminAuth map[string]string, user string) string {
	t.Helper()

	createStatus, createBody := request(t, admin, http.MethodPost, "/admin/v1/invites", fmt.Sprintf(`{"user":%q}`, user), adminAuth)
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
	clock := weekdayClock()
	public, adminServer := buildFeedApp(t, cfg, clock, fx.Replace(debugLogger))
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	adminAuth := map[string]string{"Authorization": "Bearer " + testAdminKey}

	token := redeemAgentToken(t, public, admin, adminAuth, "alice")
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
	sinceBeforeOpenedQuery := sinceQuery(clock)

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

func TestIntegration_ReviewPartyDeliversWithinTheCap(t *testing.T) {
	cfg := feedConfig(t, true)
	logged := &lockedBuffer{}
	clock := weekdayClock()
	var db *gorm.DB
	public, adminServer := buildFeedApp(t, cfg, clock, fx.Replace(slog.New(slog.NewTextHandler(logged, nil))), fx.Populate(&db))
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	adminAuth := map[string]string{"Authorization": "Bearer " + testAdminKey}
	firstMember := map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "alice")}
	secondMember := map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "bob")}
	for _, member := range []map[string]string{firstMember, secondMember} {
		status, _ := request(t, public, http.MethodPost, "/api/v1/subs", `{"repositories":["acme/api"],"type":"party"}`, member)
		require.Equal(t, http.StatusOK, status)
		require.Empty(t, listFeed(t, public, "", member))
	}
	sinceBeforeOpenedQuery := sinceQuery(clock)
	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA)))

	firstDelivered := listFeed(t, public, "", firstMember)
	secondDelivered := listFeed(t, public, "", secondMember)
	firstReceived := listFeed(t, public, sinceBeforeOpenedQuery, firstMember)
	secondReceived := listFeed(t, public, sinceBeforeOpenedQuery, secondMember)

	opened := []feedPullRequest{{Repository: feedRepository, Number: 42, HeadSHA: openedHeadSHA}}
	assert.Equal(t, opened, firstDelivered)
	assert.Empty(t, secondDelivered, "the cap of one party review is taken")
	assert.Equal(t, opened, firstReceived)
	assert.Empty(t, secondReceived, "a since read on a party repository returns only what the token received")
	var fetchedTokens int64
	require.NoError(t, db.Table("feed_fetches").Count(&fetchedTokens).Error)
	assert.Equal(t, int64(2), fetchedTokens)
	assert.Regexp(t, `msg="feed pull requests delivered" user=alice token_id=[0-9a-f]+ count=1 party=1`, logged.String())
}

func TestIntegration_ReviewPartyFlow(t *testing.T) {
	cfg := feedConfig(t, true)
	logged := &lockedBuffer{}
	clock := weekdayClock()
	public, adminServer := buildFeedApp(t, cfg, clock, fx.Replace(slog.New(slog.NewTextHandler(logged, nil))))
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	adminAuth := map[string]string{"Authorization": "Bearer " + testAdminKey}
	alice := map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "alice")}
	bob := map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "bob")}
	carol := map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "carol")}
	subscribeBodies := []struct {
		auth map[string]string
		body string
	}{
		{alice, `{"repositories":["acme/api"],"type":"party"}`},
		{bob, `{"repositories":["acme/api"],"type":"party"}`},
		{carol, `{"repositories":["acme/api"]}`},
	}
	for _, subscribe := range subscribeBodies {
		status, _ := request(t, public, http.MethodPost, "/api/v1/subs", subscribe.body, subscribe.auth)
		require.Equal(t, http.StatusOK, status)
		require.Empty(t, listFeed(t, public, "", subscribe.auth))
	}
	sinceBeforeOpenedQuery := sinceQuery(clock)

	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, numberedPullRequestEvent("opened", 42, openedHeadSHA)))
	aliceFirst := pullRequestNumbers(listFeed(t, public, "", alice))
	bobFirst := pullRequestNumbers(listFeed(t, public, "", bob))
	carolFirst := pullRequestNumbers(listFeed(t, public, "", carol))
	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, numberedPullRequestEvent("opened", 43, openedHeadSHA)))
	aliceSecond := pullRequestNumbers(listFeed(t, public, "", alice))
	bobSecond := pullRequestNumbers(listFeed(t, public, "", bob))
	aliceReceived := pullRequestNumbers(listFeed(t, public, sinceBeforeOpenedQuery, alice))
	patchStatus, patchBody := request(t, public, http.MethodPatch, "/api/v1/subs", `{"repositories":["acme/api"],"type":"solo"}`, alice)
	aliceSolo := pullRequestNumbers(listFeed(t, public, "", alice))

	assert.Equal(t, []int{42}, aliceFirst)
	assert.Empty(t, bobFirst, "the cap of one party review is taken by alice")
	assert.Equal(t, []int{42}, carolFirst, "a solo subscriber still gets every pull request")
	assert.Equal(t, []int{}, aliceSecond, "alice has load 1 and bob has load 0, so it is bob's turn")
	assert.Equal(t, []int{43}, bobSecond)
	assert.Equal(t, []int{42}, aliceReceived, "a since read returns only what alice received")
	assert.Equal(t, http.StatusOK, patchStatus)
	assert.Contains(t, patchBody, `"type":"solo"`)
	assert.Equal(t, []int{43}, aliceSolo, "a solo subscriber gets the pull request the party gave to bob")
	assert.Regexp(t, `msg="feed pull requests delivered" user=alice token_id=[0-9a-f]+ count=1 party=1`, logged.String())
}

func TestIntegration_FeedDeliversOnAWeekend(t *testing.T) {
	cfg := feedConfig(t, true)
	saturdayClock := &movingClock{start: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC), started: time.Now()}
	var db *gorm.DB
	public, adminServer := buildFeedApp(t, cfg, saturdayClock, fx.Populate(&db))
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	adminAuth := map[string]string{"Authorization": "Bearer " + testAdminKey}
	partyMember := map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "alice")}
	soloSubscriber := map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "carol")}
	subscribeBodies := []struct {
		auth map[string]string
		body string
	}{
		{partyMember, `{"repositories":["acme/api"],"type":"party"}`},
		{soloSubscriber, `{"repositories":["acme/api"]}`},
	}
	for _, subscribe := range subscribeBodies {
		status, _ := request(t, public, http.MethodPost, "/api/v1/subs", subscribe.body, subscribe.auth)
		require.Equal(t, http.StatusOK, status)
	}
	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA)))

	partyDelivered := pullRequestNumbers(listFeed(t, public, "", partyMember))
	soloDelivered := pullRequestNumbers(listFeed(t, public, "", soloSubscriber))
	var stored persistence.FeedPullRequest
	storedErr := db.Take(&stored, "repository = ? AND number = ?", feedRepository, 42).Error

	require.NoError(t, storedErr)
	require.NotNil(t, stored.RecordedAt)
	require.NotNil(t, stored.PartyDeadlineAt)
	assert.Equal(t, []int{42}, partyDelivered)
	assert.Equal(t, []int{42}, soloDelivered)
	assert.Equal(t, time.Hour, stored.PartyDeadlineAt.Sub(*stored.RecordedAt), "the party timer counts weekend hours")
}

func TestIntegration_AgentAPIDisabled_NothingTracked(t *testing.T) {
	cfg := feedConfig(t, false)
	var db *gorm.DB
	public, _ := buildAgentAPIApp(t, cfg, fx.Populate(&db))

	webhookStatus := deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA))
	unknownToken := map[string]string{"Authorization": "Bearer nct_unknown"}
	subscriptionsStatus, _ := request(t, public, http.MethodGet, "/api/v1/subs", "", unknownToken)
	pullRequestsStatus, _ := request(t, public, http.MethodGet, "/api/v1/subs/prs", "", unknownToken)
	repositoriesStatus, _ := request(t, public, http.MethodGet, "/api/v1/repos", "", unknownToken)

	var trackedPullRequests int64
	require.NoError(t, db.Table("feed_pull_requests").Count(&trackedPullRequests).Error)
	assert.Equal(t, http.StatusOK, webhookStatus)
	assert.Equal(t, http.StatusNotFound, subscriptionsStatus)
	assert.Equal(t, http.StatusNotFound, pullRequestsStatus)
	assert.Equal(t, http.StatusNotFound, repositoriesStatus)
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
	token := redeemAgentToken(t, public, admin, map[string]string{"Authorization": "Bearer " + testAdminKey}, "alice")

	status, body := request(t, public, http.MethodPost, "/api/v1/subs", `{"repositories":["acme/*"]}`,
		map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusBadRequest, status)
	assert.JSONEq(t, `{"error":"unknown_repository","repositories":["acme/*"]}`, body)
}

func TestIntegration_ListRepositories(t *testing.T) {
	cfg := feedConfig(t, true)
	cfg.Mappings = seedsToMappings(t, []mappingSeed{
		{repository: "beta/*", channel: "C123ABCDE"},
		{repository: "Acme/API", channel: "C123ABCDE"},
	})
	primeLock(t, cfg.ConfigFile, routingapp.NewProvider(routingdomain.Defaults{}, cfg.Mappings, cfg.Digest))
	public, adminServer := buildAgentAPIApp(t, cfg)
	require.NotNil(t, adminServer)
	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	token := redeemAgentToken(t, public, admin, map[string]string{"Authorization": "Bearer " + testAdminKey}, "alice")

	status, body := request(t, public, http.MethodGet, "/api/v1/repos", "", map[string]string{"Authorization": "Bearer " + token})

	assert.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `{"repositories":["acme/api"],"organizations":["beta"]}`, body)
}

func TestIntegration_ListRepositoriesNeedsAToken(t *testing.T) {
	public, _ := buildAgentAPIApp(t, feedConfig(t, true))

	status, body := request(t, public, http.MethodGet, "/api/v1/repos", "", nil)

	assert.Equal(t, http.StatusUnauthorized, status)
	assert.JSONEq(t, `{"error":"unauthorized"}`, body)
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

func seedFeedSubscriptions(t *testing.T, cfg config.Config, repositories ...string) {
	t.Helper()

	db, err := persistence.Open(cfg.DatabaseURL)
	require.NoError(t, err)
	defer func() {
		if sqlDB, closeErr := persistence.SQLDB(db); closeErr == nil {
			_ = sqlDB.Close()
		}
	}()
	ctx := context.Background()
	require.NoError(t, persistence.MigrateUp(ctx, db))
	now := time.Now()
	invites := persistence.NewAccessInvites(db)
	require.NoError(t, invites.Create(ctx, persistence.AccessInvite{
		ID: "invite", User: "alice", SecretHash: "invite-hash", ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}))
	require.NoError(t, invites.Redeem(ctx, "invite", persistence.AccessToken{
		ID: "token", User: "alice", SecretHash: "token-hash", CreatedAt: now,
	}))
	_, err = persistence.NewFeedSubscriptions(db).Add(ctx, persistence.FeedSubscriptionBatch{
		TokenID: "token", Repositories: repositories, CreatedAt: now,
	})
	require.NoError(t, err)
}

func storedFeedSubscriptions(t *testing.T, db *gorm.DB) []string {
	t.Helper()

	var repositories []string
	require.NoError(t, db.Table("feed_subscriptions").Order("repository").Pluck("repository", &repositories).Error)
	return repositories
}

func TestIntegration_StartupRemovesSubscriptionsToUnmappedRepositories(t *testing.T) {
	testCases := []struct {
		name            string
		agentAPIEnabled bool
	}{
		{name: "agent API on", agentAPIEnabled: true},
		{name: "agent API off"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := feedConfig(t, testCase.agentAPIEnabled)
			seedFeedSubscriptions(t, cfg, feedRepository, "acme/gone", "*")
			logged := &lockedBuffer{}
			var db *gorm.DB

			buildAgentAPIApp(t, cfg, fx.Replace(slog.New(slog.NewTextHandler(logged, nil))), fx.Populate(&db))

			assert.Equal(t, []string{"*", feedRepository}, storedFeedSubscriptions(t, db))
			assert.Contains(t, logged.String(), `msg="feed subscriptions removed" count=1 reason=unmapped repositories=[acme/gone]`)
		})
	}
}

const billingFilesPath = "/repos/acme/api/pulls/42/files"

type githubFilesFake struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
}

func newGitHubFilesFake(t *testing.T, filesStatus int) *githubFilesFake {
	t.Helper()

	fake := &githubFilesFake{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.paths = append(fake.paths, r.URL.Path)
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != billingFilesPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(filesStatus)
		if filesStatus == http.StatusOK {
			_, _ = w.Write([]byte(`[{"filename":"billing/x.go"}]`))
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *githubFilesFake) requestedPaths() []string {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string(nil), fake.paths...)
}

func intPointer(value int) *int { return &value }

func partyPathConfig(t *testing.T) (config.Config, *slackFake) {
	t.Helper()

	cfg := feedConfig(t, true)
	slackFake := newSlackFake(t)
	cfg.SlackBaseURL = slackFake.URL
	apiTier := cfg.Mappings["acme"]["api"]
	apiTier.Party = routingdomain.PartyOverride{Reviewers: intPointer(1)}
	apiTier.Paths = []routingdomain.PathRule{{Dir: "billing", Party: routingdomain.PartyOverride{Reviewers: intPointer(2)}}}
	cfg.Mappings["acme"]["api"] = apiTier
	cfg.Mappings["acme"]["*"] = routingdomain.RepoConfig{Party: routingdomain.PartyOverride{Deadline: durationPointer(2 * time.Hour)}}
	return cfg, slackFake
}

func durationPointer(value time.Duration) *time.Duration { return &value }

type partyMembers struct {
	alice, bob map[string]string
}

func subscribePartyMembers(t *testing.T, public *httptest.Server, adminServer *runtime.AdminServer) partyMembers {
	t.Helper()

	admin := httptest.NewServer(adminServer.Handler)
	t.Cleanup(admin.Close)
	adminAuth := map[string]string{"Authorization": "Bearer " + testAdminKey}
	members := partyMembers{
		alice: map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "alice")},
		bob:   map[string]string{"Authorization": "Bearer " + redeemAgentToken(t, public, admin, adminAuth, "bob")},
	}
	for _, member := range []map[string]string{members.alice, members.bob} {
		status, _ := request(t, public, http.MethodPost, "/api/v1/subs", `{"repositories":["acme/api"],"type":"party"}`, member)
		require.Equal(t, http.StatusOK, status)
		require.Empty(t, listFeed(t, public, "", member))
	}
	return members
}

func TestIntegration_PartySettingsFromMappings(t *testing.T) {
	cfg, slackFake := partyPathConfig(t)
	githubFake := newGitHubFilesFake(t, http.StatusOK)
	cfg.GitHubBaseURL = githubFake.URL
	cfg.GitHubToken = config.Secret("gh-read-only")
	public, adminServer := buildFeedApp(t, cfg, weekdayClock())
	require.NotNil(t, adminServer)
	members := subscribePartyMembers(t, public, adminServer)
	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA)))

	aliceDelivered := pullRequestNumbers(listFeed(t, public, "", members.alice))
	bobDelivered := pullRequestNumbers(listFeed(t, public, "", members.bob))
	starStatus, starBody := request(t, public, http.MethodPost, "/api/v1/subs", `{"repositories":["acme/other"]}`, members.alice)

	assert.Equal(t, []int{42}, aliceDelivered)
	assert.Equal(t, []int{42}, bobDelivered, "the billing path sets a cap of 2")
	assert.Equal(t, http.StatusBadRequest, starStatus, "a party-only star adds no repository")
	assert.Contains(t, starBody, "unknown_repository")
	assert.Equal(t, []string{billingFilesPath}, githubFake.requestedPaths(), "the Slack router read no files")
	postCall, posted := slackFake.findCall("/api/chat.postMessage")
	require.True(t, posted)
	assert.Equal(t, "C123ABCDE", postCall.Body["channel"])
}

func TestIntegration_PartyFilesReadFailureFallsBack(t *testing.T) {
	cfg, slackFake := partyPathConfig(t)
	githubFake := newGitHubFilesFake(t, http.StatusBadGateway)
	cfg.GitHubBaseURL = githubFake.URL
	cfg.GitHubToken = config.Secret("gh-read-only")
	logged := &lockedBuffer{}
	public, adminServer := buildFeedApp(t, cfg, weekdayClock(), fx.Replace(slog.New(slog.NewTextHandler(logged, nil))))
	require.NotNil(t, adminServer)
	members := subscribePartyMembers(t, public, adminServer)
	require.Equal(t, http.StatusOK, deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA)))

	aliceDelivered := pullRequestNumbers(listFeed(t, public, "", members.alice))
	bobDelivered := pullRequestNumbers(listFeed(t, public, "", members.bob))

	assert.Equal(t, []int{42}, aliceDelivered)
	assert.Equal(t, []int{}, bobDelivered, "the fallback takes the repository cap of 1")
	assert.Regexp(t, `msg="feed party settings fallback".*repository=acme/api.*number=42`, logged.String())
	assert.NotContains(t, logged.String(), "gh-read-only", "the read token never reaches the log")
	_, posted := slackFake.findCall("/api/chat.postMessage")
	assert.True(t, posted)
}

func TestIntegration_PartyPathRuleWithoutReadTokenUsesTheRepositoryValues(t *testing.T) {
	cfg, slackFake := partyPathConfig(t)
	cfg.GitHubToken = config.Secret("")
	public, adminServer := buildFeedApp(t, cfg, weekdayClock())
	require.NotNil(t, adminServer)
	members := subscribePartyMembers(t, public, adminServer)

	status := deliverPullRequestEvent(t, public, cfg, pullRequestEvent("opened", openedHeadSHA))
	aliceDelivered := pullRequestNumbers(listFeed(t, public, "", members.alice))
	bobDelivered := pullRequestNumbers(listFeed(t, public, "", members.bob))

	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, []int{42}, aliceDelivered)
	assert.Equal(t, []int{}, bobDelivered, "without a read token the repository cap of 1 applies")
	_, posted := slackFake.findCall("/api/chat.postMessage")
	assert.True(t, posted)
}
