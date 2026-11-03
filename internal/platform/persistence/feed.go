package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeedSubscriptions persists which repositories each token follows.
type FeedSubscriptions struct {
	db *gorm.DB
}

// NewFeedSubscriptions constructs a FeedSubscriptions repository bound to db.
func NewFeedSubscriptions(db *gorm.DB) *FeedSubscriptions {
	return &FeedSubscriptions{db: db}
}

// List returns the token's subscriptions in id order.
func (r *FeedSubscriptions) List(ctx context.Context, tokenID string) ([]FeedSubscription, error) {
	var subscriptions []FeedSubscription
	err := r.db.WithContext(ctx).Where("token_id = ?", tokenID).Order("id").Find(&subscriptions).Error
	if err != nil {
		return nil, fmt.Errorf("store: list feed subscriptions: %w", err)
	}
	return subscriptions, nil
}

// Add inserts the repositories not yet subscribed and returns how many were new.
func (r *FeedSubscriptions) Add(ctx context.Context, batch FeedSubscriptionBatch) (int, error) {
	if len(batch.Repositories) == 0 {
		return 0, nil
	}
	createdAt := batch.CreatedAt.UTC()
	rows := make([]FeedSubscription, 0, len(batch.Repositories))
	for _, repository := range batch.Repositories {
		rows = append(rows, FeedSubscription{TokenID: batch.TokenID, Repository: repository, CreatedAt: createdAt, Type: batch.Type})
	}
	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "token_id"}, {Name: "repository"}},
			DoNothing: true,
		}).
		Create(&rows)
	if result.Error != nil {
		return 0, fmt.Errorf("store: add feed subscriptions: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// Delete removes the token's subscription and returns it, or ErrNotFound.
func (r *FeedSubscriptions) Delete(ctx context.Context, tokenID string, id int64) (FeedSubscription, error) {
	var deleted []FeedSubscription
	err := r.db.WithContext(ctx).Raw(
		"DELETE FROM feed_subscriptions WHERE id = ? AND token_id = ? RETURNING id, token_id, repository, type, created_at",
		id, tokenID,
	).Scan(&deleted).Error
	if err != nil {
		return FeedSubscription{}, fmt.Errorf("store: delete feed subscription: %w", err)
	}
	if len(deleted) == 0 {
		return FeedSubscription{}, ErrNotFound
	}
	return deleted[0], nil
}

// SetType changes the type of the token's subscriptions and returns how many rows it changed.
func (r *FeedSubscriptions) SetType(ctx context.Context, change FeedTypeChange) (int, error) {
	query := r.db.WithContext(ctx).Model(&FeedSubscription{}).Where("token_id = ? AND type <> ?", change.TokenID, change.Type)
	if change.Repositories != nil {
		query = query.Where("repository IN ?", change.Repositories)
	}
	result := query.Update("type", change.Type)
	if result.Error != nil {
		return 0, fmt.Errorf("store: set feed subscription type: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// DeleteRepositories removes the repositories' subscriptions of every token and returns how many rows went.
func (r *FeedSubscriptions) DeleteRepositories(ctx context.Context, repositories []string) (int, error) {
	if len(repositories) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("repository IN ?", repositories).Delete(&FeedSubscription{})
	if result.Error != nil {
		return 0, fmt.Errorf("store: delete feed subscriptions of repositories: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// Repositories returns the distinct repositories that have a subscription, sorted.
func (r *FeedSubscriptions) Repositories(ctx context.Context) ([]string, error) {
	var repositories []string
	err := r.db.WithContext(ctx).Model(&FeedSubscription{}).
		Distinct().Order("repository").Pluck("repository", &repositories).Error
	if err != nil {
		return nil, fmt.Errorf("store: list subscribed repositories: %w", err)
	}
	return repositories, nil
}

// FeedPullRequests persists the latest state of open pull requests.
type FeedPullRequests struct {
	db *gorm.DB
}

// NewFeedPullRequests constructs a FeedPullRequests repository bound to db.
func NewFeedPullRequests(db *gorm.DB) *FeedPullRequests {
	return &FeedPullRequests{db: db}
}

// Upsert inserts or updates the pull request in place, keeping its deliveries, a known created_at and
// the first recorded_at, and replacing the party settings unless KeepStoredParty guards a recorded row.
func (r *FeedPullRequests) Upsert(ctx context.Context, pullRequest FeedPullRequest) error {
	pullRequest.UpdatedAt = pullRequest.UpdatedAt.UTC()
	if pullRequest.CreatedAt != nil {
		createdAt := pullRequest.CreatedAt.UTC()
		pullRequest.CreatedAt = &createdAt
	}
	if pullRequest.RecordedAt != nil {
		recordedAt := pullRequest.RecordedAt.UTC()
		pullRequest.RecordedAt = &recordedAt
	}
	if pullRequest.PartyDeadlineAt != nil {
		partyDeadlineAt := pullRequest.PartyDeadlineAt.UTC()
		pullRequest.PartyDeadlineAt = &partyDeadlineAt
	}
	partyColumns := []string{"party_reviewers", "party_deadline_at"}
	replacedColumns := []string{"title", "description", "url", "author_id", "source_branch", "target_branch", "head_sha", "updated_at"}
	if !pullRequest.KeepStoredParty {
		replacedColumns = append(replacedColumns, partyColumns...)
	}
	assignments := append(
		clause.AssignmentColumns(replacedColumns),
		clause.Assignment{
			Column: clause.Column{Name: "created_at"},
			Value:  gorm.Expr("COALESCE(excluded.created_at, feed_pull_requests.created_at)"),
		},
		clause.Assignment{
			Column: clause.Column{Name: "recorded_at"},
			Value:  gorm.Expr("COALESCE(feed_pull_requests.recorded_at, excluded.recorded_at)"),
		},
	)
	if pullRequest.KeepStoredParty {
		for _, column := range partyColumns {
			assignments = append(assignments, clause.Assignment{
				Column: clause.Column{Name: column},
				Value:  gorm.Expr("CASE WHEN feed_pull_requests.recorded_at IS NULL THEN excluded." + column + " ELSE feed_pull_requests." + column + " END"),
			})
		}
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "repository"}, {Name: "number"}},
			DoUpdates: assignments,
		}).
		Create(&pullRequest).Error
	if err != nil {
		return fmt.Errorf("store: upsert feed pull request: %w", err)
	}
	return nil
}

// Find returns the pull request, or ErrNotFound.
func (r *FeedPullRequests) Find(ctx context.Context, repository string, number int) (FeedPullRequest, error) {
	var pullRequest FeedPullRequest
	err := r.db.WithContext(ctx).
		Where("repository = ? AND number = ?", repository, number).
		Take(&pullRequest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return FeedPullRequest{}, ErrNotFound
	}
	if err != nil {
		return FeedPullRequest{}, fmt.Errorf("store: find feed pull request: %w", err)
	}
	return pullRequest, nil
}

// Delete removes the pull request and its deliveries; a missing row is fine.
func (r *FeedPullRequests) Delete(ctx context.Context, repository string, number int) error {
	err := r.db.WithContext(ctx).
		Where("repository = ? AND number = ?", repository, number).
		Delete(&FeedPullRequest{}).Error
	if err != nil {
		return fmt.Errorf("store: delete feed pull request: %w", err)
	}
	return nil
}

// DeleteAll removes every pull request and its deliveries and returns how many pull requests went.
func (r *FeedPullRequests) DeleteAll(ctx context.Context) (int, error) {
	result := r.db.WithContext(ctx).Exec("DELETE FROM feed_pull_requests")
	if result.Error != nil {
		return 0, fmt.Errorf("store: delete all feed pull requests: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// DeleteRepositories removes the pull requests of the repositories and their deliveries.
func (r *FeedPullRequests) DeleteRepositories(ctx context.Context, repositories []string) (int, error) {
	if len(repositories) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where("repository IN ?", repositories).Delete(&FeedPullRequest{})
	if result.Error != nil {
		return 0, fmt.Errorf("store: delete feed pull requests of repositories: %w", result.Error)
	}
	return int(result.RowsAffected), nil
}

// Repositories returns the distinct repositories that have a pull request, sorted.
func (r *FeedPullRequests) Repositories(ctx context.Context) ([]string, error) {
	var repositories []string
	err := r.db.WithContext(ctx).Model(&FeedPullRequest{}).
		Distinct().Order("repository").Pluck("repository", &repositories).Error
	if err != nil {
		return nil, fmt.Errorf("store: list feed repositories: %w", err)
	}
	return repositories, nil
}

// ChangedSince returns pull requests of the repositories updated at or after since, or delivered to the token at or after since.
func (r *FeedPullRequests) ChangedSince(ctx context.Context, query FeedSinceQuery) ([]FeedPullRequest, error) {
	if len(query.Repositories) == 0 {
		return []FeedPullRequest{}, nil
	}
	since := query.Since.UTC()
	var pullRequests []FeedPullRequest
	err := r.db.WithContext(ctx).
		Where(`repository IN ? AND (updated_at >= ? OR EXISTS (
			SELECT 1 FROM feed_deliveries own_delivery WHERE own_delivery.token_id = ?
			AND own_delivery.repository = feed_pull_requests.repository
			AND own_delivery.number = feed_pull_requests.number
			AND own_delivery.delivered_at >= ?))`, query.Repositories, since, query.TokenID, since).
		Order("updated_at, repository, number").
		Find(&pullRequests).Error
	if err != nil {
		return nil, fmt.Errorf("store: feed pull requests changed since: %w", err)
	}
	return pullRequests, nil
}

// ReceivedSince returns pull requests of the repositories that the token received, updated or delivered at or after since.
func (r *FeedPullRequests) ReceivedSince(ctx context.Context, query FeedSinceQuery) ([]FeedPullRequest, error) {
	if len(query.Repositories) == 0 {
		return []FeedPullRequest{}, nil
	}
	since := query.Since.UTC()
	var pullRequests []FeedPullRequest
	err := r.db.WithContext(ctx).
		Joins(`JOIN feed_deliveries ON feed_deliveries.repository = feed_pull_requests.repository
			AND feed_deliveries.number = feed_pull_requests.number AND feed_deliveries.token_id = ?`, query.TokenID).
		Where("feed_pull_requests.repository IN ? AND (feed_pull_requests.updated_at >= ? OR feed_deliveries.delivered_at >= ?)",
			query.Repositories, since, since).
		Order("feed_pull_requests.updated_at, feed_pull_requests.repository, feed_pull_requests.number").
		Find(&pullRequests).Error
	if err != nil {
		return nil, fmt.Errorf("store: feed pull requests received since: %w", err)
	}
	return pullRequests, nil
}

// FeedDeliveries records which pull requests each token has received.
type FeedDeliveries struct {
	db *gorm.DB
}

// NewFeedDeliveries constructs a FeedDeliveries repository bound to db.
func NewFeedDeliveries(db *gorm.DB) *FeedDeliveries {
	return &FeedDeliveries{db: db}
}

// Claim writes first so SQLite never upgrades a read lock, which fails at once with SQLITE_BUSY.
func (r *FeedDeliveries) Claim(ctx context.Context, claim FeedClaim) ([]FeedPullRequest, error) {
	if len(claim.Repositories) == 0 {
		return []FeedPullRequest{}, nil
	}
	claimed := []FeedPullRequest{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inserted []FeedPullRequestKey
		insertErr := tx.Raw(
			`INSERT INTO feed_deliveries (token_id, repository, number, delivered_at)
			 SELECT ?, repository, number, ? FROM feed_pull_requests WHERE repository IN ?
			 ON CONFLICT DO NOTHING RETURNING repository, number`,
			claim.TokenID, claim.DeliveredAt.UTC(), claim.Repositories,
		).Scan(&inserted).Error
		if insertErr != nil {
			return fmt.Errorf("insert deliveries: %w", insertErr)
		}
		if len(inserted) == 0 {
			return nil
		}
		readErr := tx.Where("(repository, number) IN ?", keyArguments(inserted)).
			Order("updated_at, repository, number").
			Find(&claimed).Error
		if readErr != nil {
			return fmt.Errorf("read claimed pull requests: %w", readErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: claim feed pull requests: %w", err)
	}
	return claimed, nil
}

const (
	partySubscriptionType = "party"
	wildcardRepository    = "*"
)

const partyMembersSQL = `WITH claim_repositories(repository) AS (SELECT value FROM json_each(?))
	SELECT DISTINCT claim_repositories.repository AS repository, subscription.token_id AS token_id
	FROM claim_repositories
	JOIN feed_subscriptions subscription ON subscription.repository = claim_repositories.repository
		OR (subscription.repository = ? AND NOT EXISTS (
			SELECT 1 FROM feed_subscriptions own_subscription
			WHERE own_subscription.token_id = subscription.token_id
			AND own_subscription.repository = claim_repositories.repository))
	JOIN feed_polls poll ON poll.token_id = subscription.token_id AND poll.polled_at >= ?
	WHERE subscription.type = ? AND subscription.token_id <> ?
	ORDER BY claim_repositories.repository, subscription.token_id`

const notReceivedPartySQL = `feed_pull_requests.repository IN ? AND NOT EXISTS (
		SELECT 1 FROM feed_deliveries own_delivery WHERE own_delivery.token_id = ?
		AND own_delivery.repository = feed_pull_requests.repository
		AND own_delivery.number = feed_pull_requests.number)`

const partyDeliveryCountSQL = `(SELECT COUNT(*) FROM feed_deliveries party_delivery WHERE party_delivery.party
		AND party_delivery.repository = feed_pull_requests.repository
		AND party_delivery.number = feed_pull_requests.number)`

const keysFromJSONSQL = `(SELECT json_extract(value, '$.repository'), json_extract(value, '$.number') FROM json_each(?))`

type partyMemberRow struct {
	Repository string
	TokenID    string
}

// ClaimParty reads the snapshot and records what Choose picks in one transaction that writes first,
// so members that poll at the same moment never pass the cap.
func (r *FeedDeliveries) ClaimParty(ctx context.Context, claim FeedPartyClaim) (FeedPartyClaimResult, error) {
	result := FeedPartyClaimResult{Claimed: []FeedPullRequest{}}
	if len(claim.Repositories) == 0 {
		return result, nil
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		deleteErr := tx.Exec("DELETE FROM feed_party_loads WHERE day < ?", claim.Day).Error
		if deleteErr != nil {
			return fmt.Errorf("delete earlier party loads: %w", deleteErr)
		}
		taken, takenErr := countTakenPartyPullRequests(tx, claim)
		if takenErr != nil {
			return takenErr
		}
		result.Taken = taken
		snapshot, snapshotErr := readPartySnapshot(tx, claim)
		if snapshotErr != nil {
			return snapshotErr
		}
		if len(snapshot.Waiting) == 0 {
			return nil
		}
		chosen := keepWaitingKeys(snapshot.Waiting, claim.Choose(snapshot))
		if len(chosen) == 0 {
			return nil
		}
		inserted, insertErr := insertPartyDeliveries(tx, claim, chosen)
		if insertErr != nil {
			return insertErr
		}
		if len(inserted) == 0 {
			return nil
		}
		if loadErr := addPartyLoads(tx, claim, inserted); loadErr != nil {
			return loadErr
		}
		claimed, readErr := readPullRequestsInOrder(tx, inserted)
		if readErr != nil {
			return readErr
		}
		result.Claimed = claimed
		return nil
	})
	if err != nil {
		return FeedPartyClaimResult{}, fmt.Errorf("store: claim feed party pull requests: %w", err)
	}
	return result, nil
}

func countTakenPartyPullRequests(tx *gorm.DB, claim FeedPartyClaim) (int, error) {
	var taken int64
	err := tx.Table("feed_pull_requests").
		Where(notReceivedPartySQL+" AND "+partyDeliveryCountSQL+" >= feed_pull_requests.party_reviewers",
			claim.Repositories, claim.TokenID).
		Count(&taken).Error
	if err != nil {
		return 0, fmt.Errorf("count taken party pull requests: %w", err)
	}
	return int(taken), nil
}

func readPartySnapshot(tx *gorm.DB, claim FeedPartyClaim) (FeedPartySnapshot, error) {
	var snapshot FeedPartySnapshot
	waitingErr := tx.Table("feed_pull_requests").
		Select("repository, number, updated_at, party_reviewers, party_deadline_at").
		Where(notReceivedPartySQL+" AND "+partyDeliveryCountSQL+" < feed_pull_requests.party_reviewers",
			claim.Repositories, claim.TokenID).
		Order("updated_at, repository, number").
		Find(&snapshot.Waiting).Error
	if waitingErr != nil {
		return FeedPartySnapshot{}, fmt.Errorf("read waiting pull requests: %w", waitingErr)
	}
	if len(snapshot.Waiting) == 0 {
		return snapshot, nil
	}
	waitingKeys, encodeErr := keysJSON(waitingKeysOf(snapshot.Waiting))
	if encodeErr != nil {
		return FeedPartySnapshot{}, encodeErr
	}
	deliveriesErr := tx.Where("(repository, number) IN "+keysFromJSONSQL, waitingKeys).
		Order("repository, number, token_id").
		Find(&snapshot.Deliveries).Error
	if deliveriesErr != nil {
		return FeedPartySnapshot{}, fmt.Errorf("read deliveries of waiting pull requests: %w", deliveriesErr)
	}
	repositories := waitingRepositories(snapshot.Waiting)
	members, membersErr := readPartyMembers(tx, claim, repositories)
	if membersErr != nil {
		return FeedPartySnapshot{}, membersErr
	}
	snapshot.Members = members
	loadsErr := tx.Where("day = ? AND repository IN ?", claim.Day, repositories).
		Order("repository, token_id").
		Find(&snapshot.Loads).Error
	if loadsErr != nil {
		return FeedPartySnapshot{}, fmt.Errorf("read party loads: %w", loadsErr)
	}
	return snapshot, nil
}

func readPartyMembers(tx *gorm.DB, claim FeedPartyClaim, repositories []string) (map[string][]string, error) {
	encodedRepositories, encodeErr := json.Marshal(repositories)
	if encodeErr != nil {
		return nil, fmt.Errorf("encode party repositories: %w", encodeErr)
	}
	var rows []partyMemberRow
	readErr := tx.Raw(partyMembersSQL,
		string(encodedRepositories), wildcardRepository, claim.ActiveSince.UTC(), partySubscriptionType, claim.TokenID,
	).Scan(&rows).Error
	if readErr != nil {
		return nil, fmt.Errorf("read party members: %w", readErr)
	}
	membersByRepository := map[string][]string{}
	for _, row := range rows {
		membersByRepository[row.Repository] = append(membersByRepository[row.Repository], row.TokenID)
	}
	return membersByRepository, nil
}

func waitingKeysOf(waiting []FeedWaitingPullRequest) []FeedPullRequestKey {
	keys := make([]FeedPullRequestKey, 0, len(waiting))
	for _, pullRequest := range waiting {
		keys = append(keys, waitingKey(pullRequest))
	}
	return keys
}

func waitingRepositories(waiting []FeedWaitingPullRequest) []string {
	isListed := map[string]bool{}
	repositories := []string{}
	for _, pullRequest := range waiting {
		if !isListed[pullRequest.Repository] {
			isListed[pullRequest.Repository] = true
			repositories = append(repositories, pullRequest.Repository)
		}
	}
	return repositories
}

func keepWaitingKeys(waiting []FeedWaitingPullRequest, chosen []FeedPullRequestKey) []FeedPullRequestKey {
	notYetKept := make(map[FeedPullRequestKey]bool, len(waiting))
	for _, pullRequest := range waiting {
		notYetKept[waitingKey(pullRequest)] = true
	}
	kept := make([]FeedPullRequestKey, 0, len(chosen))
	for _, key := range chosen {
		if notYetKept[key] {
			kept = append(kept, key)
			delete(notYetKept, key)
		}
	}
	return kept
}

func insertPartyDeliveries(tx *gorm.DB, claim FeedPartyClaim, chosen []FeedPullRequestKey) ([]FeedPullRequestKey, error) {
	chosenKeys, encodeErr := keysJSON(chosen)
	if encodeErr != nil {
		return nil, encodeErr
	}
	var insertedKeys []FeedPullRequestKey
	insertErr := tx.Raw(
		`INSERT INTO feed_deliveries (token_id, repository, number, delivered_at, party)
		 SELECT ?, repository, number, ?, 1 FROM feed_pull_requests WHERE (repository, number) IN `+keysFromJSONSQL+`
		 ON CONFLICT DO NOTHING RETURNING repository, number`,
		claim.TokenID, claim.DeliveredAt.UTC(), chosenKeys,
	).Scan(&insertedKeys).Error
	if insertErr != nil {
		return nil, fmt.Errorf("insert party deliveries: %w", insertErr)
	}
	isInserted := make(map[FeedPullRequestKey]bool, len(insertedKeys))
	for _, key := range insertedKeys {
		isInserted[key] = true
	}
	inserted := make([]FeedPullRequestKey, 0, len(insertedKeys))
	for _, key := range chosen {
		if isInserted[key] {
			inserted = append(inserted, key)
		}
	}
	return inserted, nil
}

func addPartyLoads(tx *gorm.DB, claim FeedPartyClaim, inserted []FeedPullRequestKey) error {
	loads := []FeedPartyLoad{}
	loadIndexByRepository := map[string]int{}
	for _, key := range inserted {
		loadIndex, found := loadIndexByRepository[key.Repository]
		if !found {
			loadIndex = len(loads)
			loadIndexByRepository[key.Repository] = loadIndex
			loads = append(loads, FeedPartyLoad{TokenID: claim.TokenID, Repository: key.Repository, Day: claim.Day})
		}
		loads[loadIndex].Taken++
	}
	err := tx.
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "day"}, {Name: "repository"}, {Name: "token_id"}},
			DoUpdates: clause.Assignments(map[string]any{"taken": gorm.Expr("feed_party_loads.taken + excluded.taken")}),
		}).
		Create(&loads).Error
	if err != nil {
		return fmt.Errorf("add party loads: %w", err)
	}
	return nil
}

func readPullRequestsInOrder(tx *gorm.DB, keys []FeedPullRequestKey) ([]FeedPullRequest, error) {
	encodedKeys, encodeErr := keysJSON(keys)
	if encodeErr != nil {
		return nil, encodeErr
	}
	var found []FeedPullRequest
	if err := tx.Where("(repository, number) IN "+keysFromJSONSQL, encodedKeys).Find(&found).Error; err != nil {
		return nil, fmt.Errorf("read claimed pull requests: %w", err)
	}
	foundByKey := make(map[FeedPullRequestKey]FeedPullRequest, len(found))
	for _, pullRequest := range found {
		foundByKey[pullRequestKey(pullRequest)] = pullRequest
	}
	ordered := make([]FeedPullRequest, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, foundByKey[key])
	}
	return ordered, nil
}

func keysJSON(keys []FeedPullRequestKey) (string, error) {
	encoded, err := json.Marshal(keys)
	if err != nil {
		return "", fmt.Errorf("encode pull request keys: %w", err)
	}
	return string(encoded), nil
}

func waitingKey(pullRequest FeedWaitingPullRequest) FeedPullRequestKey {
	return FeedPullRequestKey{Repository: pullRequest.Repository, Number: pullRequest.Number}
}

func pullRequestKey(pullRequest FeedPullRequest) FeedPullRequestKey {
	return FeedPullRequestKey{Repository: pullRequest.Repository, Number: pullRequest.Number}
}

func keyArguments(keys []FeedPullRequestKey) [][]any {
	arguments := make([][]any, 0, len(keys))
	for _, key := range keys {
		arguments = append(arguments, []any{key.Repository, key.Number})
	}
	return arguments
}

// FeedPolls records when each token last polled the feed.
type FeedPolls struct {
	db *gorm.DB
}

// NewFeedPolls constructs a FeedPolls repository bound to db.
func NewFeedPolls(db *gorm.DB) *FeedPolls {
	return &FeedPolls{db: db}
}

// Record stores the poll time of the token, replacing a stored time before ReplaceBefore.
func (r *FeedPolls) Record(ctx context.Context, record FeedPollRecord) error {
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "token_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"polled_at"}),
			Where: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "feed_polls.polled_at < ?", Vars: []any{record.ReplaceBefore.UTC()}},
			}},
		}).
		Create(&FeedPoll{TokenID: record.TokenID, PolledAt: record.PolledAt.UTC()}).Error
	if err != nil {
		return fmt.Errorf("store: record feed poll: %w", err)
	}
	return nil
}
