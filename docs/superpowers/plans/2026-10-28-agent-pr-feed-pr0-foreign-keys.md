# Foreign keys on every pooled connection — Implementation Plan (agent PR feed, PR 0)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** SQLite enforces foreign keys on every pooled connection, not only on the one that ran `PRAGMA foreign_keys = ON`.

**Architecture:** `persistence.Open` adds `_pragma=foreign_keys(1)` to the DSN, so the `glebarez/go-sqlite` driver runs the pragma on each new connection. The one-shot `enableForeignKeys` exec goes away.

**Tech Stack:** Go 1.27.3, GORM, `github.com/glebarez/sqlite` v1.11.0 (driver `glebarez/go-sqlite` v1.21.2), testify.

**Spec:** `docs/superpowers/specs/2026-10-27-agent-pr-feed-design.md` (section "Delivery in 3 PRs", item 0).

## Global Constraints

- PR title / squash commit: `fix(persistence): enable foreign keys on every pooled connection`.
- `just check` passes (vet, golangci-lint incl. testifylint, govulncheck, race tests, build).
- Test style per `CLAUDE.md`: testify, `(t, want, got)`, arrange / act / assert, `require` for setup.
- No comments that restate code. No new dependencies.

## Review Focus

- An operator `database.url` that already has a query string (`file:./data/notifycat.db?_pragma=busy_timeout(5000)`) keeps its own pragmas and also gets foreign keys — Task 1 pins it.
- The bare path form (`./data/notifycat.db`, no `file:`) still works — Task 1 pins it.
- All existing migrations still apply with foreign keys on for every connection — the full `just test` run covers it (every store test calls `NewTestDB`, which runs `MigrateUp`).
- A cascade delete works on a connection that is not the first one — Task 1 pins it.
- Nothing else in the repo calls `enableForeignKeys` — `go build ./...` covers it.

---

### Task 1: DSN pragma for foreign keys

**Files:**
- Modify: `internal/platform/persistence/db.go` (`Open`, remove `enableForeignKeys`, add `withForeignKeys`)
- Create: `internal/platform/persistence/db_test.go`
- Add to git: `docs/superpowers/specs/2026-10-27-agent-pr-feed-design.md`, this plan

**Interfaces:**
- Consumes: `persistence.Open(url string) (*gorm.DB, error)`, `persistence.SQLDB`, `persistence.NewTestDB`.
- Produces: unchanged public API. Unexported `withForeignKeys(dsn string) string`: appends `_pragma=foreign_keys(1)` with `?` when the DSN has no query, `&` when it has one.

- [ ] **Step 1: Write the failing tests** in `internal/platform/persistence/db_test.go` (package `persistence_test`)

```go
func TestOpen_EnablesForeignKeysOnEveryPooledConnection(t *testing.T) {
	db := persistence.NewTestDB(t)
	connections := holdConnections(t, db, 3)

	enabled := foreignKeysPerConnection(t, connections)

	assert.Equal(t, []int{1, 1, 1}, enabled)
}

func TestOpen_KeepsOperatorQueryParameters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query.db")
	db, err := persistence.Open("file:" + path + "?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	connections := holdConnections(t, db, 2)

	enabled := foreignKeysPerConnection(t, connections)
	busyTimeout := pragmaValue(t, connections[1], "busy_timeout")

	assert.Equal(t, []int{1, 1}, enabled)
	assert.Equal(t, 5000, busyTimeout)
}

func TestOpen_AcceptsBarePath(t *testing.T) {
	db, err := persistence.Open(filepath.Join(t.TempDir(), "bare.db"))
	require.NoError(t, err)
	connections := holdConnections(t, db, 2)

	enabled := foreignKeysPerConnection(t, connections)

	assert.Equal(t, []int{1, 1}, enabled)
}

func TestOpen_CascadesOnASecondConnection(t *testing.T) {
	db := persistence.NewTestDB(t)
	connections := holdConnections(t, db, 2)
	ctx := context.Background()
	// seed through connections[1]: a pull_requests row and one slack_messages row
	// that references it (columns from migrations/00005_pull_requests_messages.sql)

	_, err := connections[1].ExecContext(ctx, "DELETE FROM pull_requests")

	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, connections[1], "slack_messages"),
		"deleting a PR must cascade on any pooled connection")
}
```

Helpers in the same file:
- `holdConnections(t, db *gorm.DB, count int) []*sql.Conn` — calls `sqlDB.Conn(ctx)` `count` times **without** releasing, so each is a distinct pooled connection; `t.Cleanup` closes them.
- `foreignKeysPerConnection(t, connections []*sql.Conn) []int` — `PRAGMA foreign_keys` per connection.
- `pragmaValue(t, connection *sql.Conn, name string) int`.
- `countRows(t, connection *sql.Conn, table string) int`.

Read `migrations/00005_pull_requests_messages.sql` for the exact `pull_requests` / `slack_messages` columns before writing the seed `INSERT`s.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -race ./internal/platform/persistence/ -run 'TestOpen_' -v`
Expected: FAIL. `foreign_keys` is `0` on every connection after the first (for example `[1 0 0]`), and the cascade test finds a leftover `slack_messages` row.

- [ ] **Step 3: Implement** in `internal/platform/persistence/db.go`

`Open` passes `withForeignKeys(stripFilePrefix(url))` to `sqlite.Open` and no longer calls `enableForeignKeys`. Delete `enableForeignKeys`. `withForeignKeys` uses `strings.Contains(dsn, "?")` to pick `&` or `?`; the pragma value lives in an unexported constant (`foreignKeysPragma = "_pragma=foreign_keys(1)"`).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/platform/persistence/ -run 'TestOpen_' -v`
Expected: PASS.

- [ ] **Step 5: Run the full verification**

Run: `just check`
Expected: exit 0. Every package that uses `NewTestDB` still passes, so all migrations apply with foreign keys on.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/persistence/db.go internal/platform/persistence/db_test.go \
  docs/superpowers/specs/2026-10-27-agent-pr-feed-design.md \
  docs/superpowers/plans/2026-10-28-agent-pr-feed-pr0-foreign-keys.md
git commit -m "fix(persistence): enable foreign keys on every pooled connection"
```
