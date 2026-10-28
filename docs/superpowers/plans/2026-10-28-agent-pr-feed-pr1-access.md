# Admin port, invites and agent tokens — Implementation Plan (agent PR feed, PR 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An admin on a private admin port invites a developer; the developer redeems the one-hour invite on the public API for their own bcrypt-hashed bearer token, and a bearer middleware turns that token into a `Principal` for the `/api/v1/` routes PR 2 adds.

**Architecture:** A new `internal/access` domain (template: `internal/review`) owns the credential model, five use cases, the persistence adapters, and the HTTP adapters. `internal/runtime` composes `access.Module` (the first domain module the runtime composes directly), serves the admin routes on a second `http.Server` bound to `server.admin_addr`, and registers the redeem route plus a bearer-protected `/api/v1/` route group on the public mux — all only when `agent_api.enabled`. Migration `00010` adds `access_invites` and `access_tokens`; `cmd/notifycat-keygen` prints an admin key and its hash.

**Tech Stack:** Go 1.27.3, uber/fx v1.24.0, GORM + `github.com/glebarez/sqlite`, goose v3, `golang.org/x/crypto/bcrypt` (new dependency), testify.

**Spec:** `docs/superpowers/specs/2026-10-27-agent-pr-feed-design.md` — "Delivery in 3 PRs" item 1, plus the sections "Admin port", "Public API" (redeem row only), "Security rules", "`internal/access`", "Persistence" (the two `-- PR 1` tables), "Config", "Runtime wiring" (without the fan-out), "`cmd/notifycat-keygen`", "Logs (new)", "Verification" (the access lines).

## Global Constraints

- DDD + hexagonal + uber/fx per CLAUDE.md: every use case and every infrastructure service has an interface in `internal/access/domain/interfaces.go`; enums in `enums.go`, constants in `constants.go`; >3 args to an exported func/constructor → a domain DTO; doc comments on interfaces only; one `fx.Module`.
- Readable names (no `m`, `d`, `rc`, `tc`); table tests use `testCases`/`testCase`.
- testify; `(t, want, got)`; arrange / act / assert; `require` for setup, `assert` for final checks; only `assert` inside httptest handlers.
- Minimal comments: none that restate code.
- Conventional Commits; never mention AI/Claude in commit messages; no Co-Authored-By trailer. PR title / squash commit: `feat(access): admin port, invites and agent tokens`.
- Tokens, invites, the admin key, and `Authorization` headers never reach logs.
- Foreign keys are ON on every connection (PR 0). A migration must never rebuild a parent table (create-copy-DROP-rename): goose runs each migration in a transaction where `PRAGMA foreign_keys=OFF` does nothing, so the DROP cascade-deletes child rows.
- `just check` passes at the end. In this environment `just` and a working local golangci-lint are missing: the plan's verification steps must use `go vet ./...`, `go test -race ./...`, `go build ./...`, and lint via `docker run --rm -v "$PWD":/app -w /app -v "$(go env GOMODCACHE)":/go/pkg/mod golangci/golangci-lint:v2.14.0 golangci-lint run ./...`.
- Formats (spec call 3): token `nct_<id>_<secret>`, invite `nci_<id>_<secret>`, admin key `nca_<secret>`. `id` = 16 random bytes, lower-case hex (32 chars); `secret` = 32 random bytes, base64url without padding (43 chars); both from `crypto/rand`. The DB stores `id` in clear and `bcrypt(secret)`. bcrypt cost `bcrypt.DefaultCost` (10). No in-memory cache of verified tokens.
- Invite: single use, valid for `time.Hour`. User labels: `^[a-z0-9][a-z0-9._-]{0,63}$`.
- Every auth failure answers the same `401 {"error":"unauthorized"}`; only the log says why. Every JSON response carries `Cache-Control: no-store`; errors are `{"error":"<code>"}`.
- Log messages and fields exactly as the spec's "Logs (new)" (the access lines). No returned or logged error embeds a raw credential.
- **Task gate** (the "run the task gate" step in each task):

  ```bash
  go vet ./... && go test -race ./... && go build ./...
  docker run --rm -v "$PWD":/app -w /app -v "$(go env GOMODCACHE)":/go/pkg/mod golangci/golangci-lint:v2.14.0 golangci-lint run ./...
  ```

  Expected: every command exits 0.

## Decisions the spec leaves open (settled here)

1. The call-8 startup checks run in `config.Load` and are unconditional (also when `agent_api.enabled` is false). `admin_addr` set while the API is disabled starts no admin server and logs `WARN admin server not started reason="agent_api.enabled is false"`.
2. One pair of surrounding single quotes is stripped from `NOTIFYCAT_ADMIN_KEY_HASH` (`docker run --env-file` passes quotes literally); an invalid hash fails startup with a hint to single-quote it.
3. `redeem_command` is `curl -fsS -X POST -H @- https://<server.domain>/api/v1/invites/redeem <<<'Authorization: Invite <invite>'`; with an empty `server.domain` the URL is `"$NOTIFYCAT_URL/api/v1/invites/redeem"` (the agent-setup variable).
4. Invite check order: parse → find → bcrypt → used → expired. An invite is expired when `now >= expires_at`. A malformed invite logs `reason=malformed` too.
5. A store failure is not an auth failure: `500 {"error":"internal_error"}`, logged at ERROR (`agent api request failed` / `admin request failed`).
6. `last_used_at` is throttled in the use case from the loaded row; a failed touch logs `WARN agent token last_used_at not updated` and does not fail the request. Two first calls at the same instant may both write — harmless.
7. The whole admin mux sits behind the admin key: an unknown path on the admin port answers 401 before 404. `WWW-Authenticate: Bearer` on admin and bearer 401s, `WWW-Authenticate: Invite` on redeem 401s.
8. Timestamps are RFC 3339, UTC, second precision. `last_used_at` is `null` until first use. Tokens list oldest `created_at` first.
9. Request bodies are capped at 16 KiB; bad JSON → `400 invalid_body`; a bad label → `400 invalid_user` (no lower-casing).
10. The `Authorization` scheme is matched case-insensitively and the credential is trimmed.

## Review Focus

- A secret that contains `_` or `-` (base64url) must parse: a credential is split by fixed lengths, never on `_` — Task 2 pins it.
- `NOTIFYCAT_ADMIN_KEY_HASH` mangled by env handling (quotes kept by `docker run --env-file`, `$` expanded in an unquoted Compose value) must strip the quotes or fail startup with a clear hint, never boot with an admin key that never matches — Task 1 pins it.
- A token pasted from an rc file with a trailing newline or a lower-case `bearer` scheme must still authenticate; an invite sent as `Bearer` or a token sent as `Invite` must 401 — Task 5 pins it.
- An admin port that cannot bind (in use, or `0.0.0.0:9091` while `server.addr` is `:9091`) must stop the process with exit code 1, not leave a server without its admin side — Task 6 pins it.
- Two redeems of one invite racing past the use case's "unused" check must yield exactly one token: the store's conditional update answers `ErrInviteUsed` — Task 3 pins it.

## Seams for PR 2

- `accessdomain.Principal{TokenID, User}`, `accessdomain.WithPrincipal`, `accessdomain.PrincipalFrom(ctx) (Principal, bool)` (Task 2).
- `httpx.WriteJSON`, `httpx.WriteError`, `httpx.WriteNoContent` (Task 5) and `httpx.Route{Pattern, Handler}` (Task 6).
- `runtime.AsAgentAPIRoutes(constructor)`: any constructor returning `[]httpx.Route` joins the group the runtime registers on the public mux behind the bearer middleware when `agent_api.enabled` (Task 6). PR 2 adds `fx.Provide(AsAgentAPIRoutes(buildFeedRoutes))`.
- `access_tokens(id TEXT PRIMARY KEY)` for PR 2's `ON DELETE CASCADE` foreign keys (Task 3). PR 2's concurrent `Claim` test will need a `busy_timeout` pragma; this PR adds none.

---

### Task 1: Config keys, admin key hash, startup checks

**Files:**
- Modify: `internal/platform/config/config.go` (`Config`, `fileSchema`, `applyFileSchema`, `readSecrets`, `Load`; new `validateAgentAPI`)
- Modify: `internal/platform/config/config_test.go` (`writeConfig` also clears `NOTIFYCAT_ADMIN_KEY_HASH`; new tests)
- Modify: `cmd/notifycat-server/startup_test.go` (`TestStartupError_MissingSecret` table)
- Modify: `config.example.yaml`, `.env.example`, `docs/configuration.md`, `go.mod`, `go.sum`
- Add to git: this plan

**Interfaces:**
- Consumes: `config.Secret`, `config.MissingVarError`.
- Produces: `Config.AdminAddr string`, `Config.AgentAPIEnabled bool`, `Config.AdminKeyHash Secret` (quotes already stripped, validated). `Load` returns `*MissingVarError{Var: "NOTIFYCAT_ADMIN_KEY_HASH"}` when `admin_addr` is set without the hash.

- [ ] **Step 1: Add the dependency**

Run: `go get golang.org/x/crypto@latest && go mod tidy`
Expected: `go.mod` lists `golang.org/x/crypto` as a direct requirement after Step 3 uses it.

- [ ] **Step 2: Write the failing tests** in `internal/platform/config/config_test.go`

Add `"NOTIFYCAT_ADMIN_KEY_HASH"` to the list `writeConfig` clears. Helper `adminKeyHash(t) string` = `bcrypt.GenerateFromPassword([]byte("nca_test"), bcrypt.MinCost)`. Tests that set the hash call `writeConfig` + `setSecrets` + `t.Setenv(...)` + `config.Load()` (not `loadConfigured`, which would clear it).

```go
func TestLoad_AgentAPI_DefaultsOff(t *testing.T)            // minimalConfig
	assert.False(t, cfg.AgentAPIEnabled)
	assert.Empty(t, cfg.AdminAddr)
	assert.Empty(t, cfg.AdminKeyHash.Reveal())

func TestLoad_AgentAPI_ReadsKeysAndHash(t *testing.T)
	// minimalConfig + "  admin_addr: \"127.0.0.1:9091\"\nagent_api:\n  enabled: true\n"
	assert.True(t, cfg.AgentAPIEnabled)
	assert.Equal(t, "127.0.0.1:9091", cfg.AdminAddr)
	assert.Equal(t, hash, cfg.AdminKeyHash.Reveal())
	assert.Equal(t, "[REDACTED]", cfg.AdminKeyHash.String())

func TestLoad_AdminKeyHash_StripsSingleQuotes(t *testing.T)  // env value "'" + hash + "'"
	assert.Equal(t, hash, cfg.AdminKeyHash.Reveal())

func TestLoad_AdminAddrWithoutKeyHash_Fails(t *testing.T)   // admin_addr set, agent_api absent (disabled)
	var missing *config.MissingVarError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "NOTIFYCAT_ADMIN_KEY_HASH", missing.Var, "the check runs even while the API is disabled")

func TestLoad_AdminAddrEqualsAddr_Fails(t *testing.T)       // addr ":9091", admin_addr ":9091", valid hash
	assert.ErrorContains(t, err, "server.admin_addr")
	assert.ErrorContains(t, err, "server.addr")

func TestLoad_AdminKeyHashNotBcrypt_Fails(t *testing.T)     // table, admin_addr set
	// testCases: {"plain text", "not-a-hash"},
	//            {"dollar signs expanded", strings.ReplaceAll(hash, "$", "")},
	//            {"cut after the cost", "$2a$10$"}
	assert.ErrorContains(t, err, "NOTIFYCAT_ADMIN_KEY_HASH")
	assert.ErrorContains(t, err, "single quotes")

func TestLoad_AgentAPI_UnknownKeyRejected(t *testing.T)     // "agent_api:\n  enable: true\n"
	require.Error(t, err)
```

In `cmd/notifycat-server/startup_test.go`, add `"NOTIFYCAT_ADMIN_KEY_HASH"` to the `TestStartupError_MissingSecret` loop (no production change there: `startupError` already handles any `MissingVarError`).

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test -race ./internal/platform/config/ -run 'AgentAPI|AdminAddr|AdminKeyHash' -v`
Expected: FAIL to compile — `cfg.AgentAPIEnabled`, `cfg.AdminAddr`, `cfg.AdminKeyHash` undefined.

- [ ] **Step 4: Implement** in `internal/platform/config/config.go`

- `Config` gains `AdminAddr string`, `AgentAPIEnabled bool`, `AdminKeyHash Secret`.
- `fileSchema.Server` gains `AdminAddr string \`yaml:"admin_addr"\``; `fileSchema` gains `AgentAPI struct { Enabled *bool \`yaml:"enabled"\` } \`yaml:"agent_api"\``. `applyFileSchema` copies both (default `false`, `""`).
- `readSecrets` sets `cfg.AdminKeyHash = Secret(trimSingleQuotes(os.Getenv("NOTIFYCAT_ADMIN_KEY_HASH")))`; `trimSingleQuotes` removes one pair only when the value both starts and ends with `'`.
- `validateAgentAPI(cfg Config) error`, called in `Load` right after `readSecrets`, in this order: `AdminAddr != "" && AdminKeyHash == ""` → `fmt.Errorf("config: %w", &MissingVarError{Var: "NOTIFYCAT_ADMIN_KEY_HASH"})`; `AdminAddr != "" && AdminAddr == Addr` → `config: server.admin_addr must differ from server.addr (both %q)`; `AdminKeyHash != ""` and `bcrypt.Cost([]byte(hash))` errors → `config: NOTIFYCAT_ADMIN_KEY_HASH is not a valid bcrypt hash — wrap it in single quotes in .env so $ is not expanded: %w`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -race ./internal/platform/config/ ./cmd/notifycat-server/ -v`
Expected: PASS.

- [ ] **Step 6: Config example and docs**

- `config.example.yaml`: under `server:` add `admin_addr: ""` with the comment `# private admin port for agent invites/tokens (agent_api); empty = no admin server; never publish it`; add a top-level `agent_api:` block with `enabled: false` and a one-line comment.
- `.env.example`: commented entry `# NOTIFYCAT_ADMIN_KEY_HASH='$2a$10$...'` with one comment line: generated by `notifycat-keygen`, required when `server.admin_addr` is set, keep the single quotes.
- `docs/configuration.md`: secrets table row for `NOTIFYCAT_ADMIN_KEY_HASH` (required when `server.admin_addr` is set; must be single-quoted; generated by `notifycat-keygen`); `server.admin_addr` row in the `server` table (default unset, must differ from `server.addr`, plain HTTP, never published); `server.domain` row now also says the invite `redeem_command` uses it; new `### agent_api` section (`agent_api.enabled`, default `false`; off = no `/api/v1/` routes and no admin server); the fail-fast paragraph lists the three new startup failures.

- [ ] **Step 7: Run the task gate** (Global Constraints). Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/platform/config/ cmd/notifycat-server/startup_test.go \
  config.example.yaml .env.example docs/configuration.md \
  docs/superpowers/plans/2026-10-28-agent-pr-feed-pr1-access.md
git commit -m "feat(config): add admin_addr, agent_api.enabled and NOTIFYCAT_ADMIN_KEY_HASH"
```

---

### Task 2: Access domain contracts, credential parsing, secrets, hasher, clock

**Files:**
- Create: `internal/access/domain/{doc.go,constants.go,enums.go,errors.go,models.go,credential.go,interfaces.go}`
- Create: `internal/access/domain/credential_test.go`, `internal/access/domain/models_test.go`
- Create: `internal/access/infrastructure/{doc.go,random_secret.go,bcrypt_hasher.go,system_clock.go}`
- Create: `internal/access/infrastructure/primitives_test.go`
- Create: `internal/access/application/doc.go` (package doc only, so the depguard rule has a target)
- Modify: `.golangci.yml` (rules `access-domain`, `access-application`, copied from `review-domain` / `review-application` with `access` paths)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces (package `domain`, imported elsewhere as `accessdomain`):
  - `constants.go`: `TokenPrefix = "nct_"`, `InvitePrefix = "nci_"`, `AdminKeyPrefix = "nca_"`, `CredentialSeparator = "_"`, `IDBytes = 16`, `SecretBytes = 32`, `EncodedIDLength = 2 * IDBytes`, `EncodedSecretLength = 43`, `InviteTTL = time.Hour`, `LastUsedResolution = time.Minute`, `UserLabelPattern = "^[a-z0-9][a-z0-9._-]{0,63}$"`, `BearerScheme = "Bearer"`, `InviteScheme = "Invite"`, `RedeemPath = "/api/v1/invites/redeem"`, `RedeemPattern = "POST " + RedeemPath`, `CreateInvitePattern = "POST /admin/v1/invites"`, `ListTokensPattern = "GET /admin/v1/tokens"`, `DeleteTokenPattern = "DELETE /admin/v1/tokens/{id}"`, `TokenIDPathValue = "id"`, `MaxRequestBodyBytes = 16 << 10`, `PublicURLVariable = "$NOTIFYCAT_URL"`.
  - `enums.go`: `type AuthFailureReason string` — `AuthFailureMalformed "malformed"`, `AuthFailureUnknown "unknown"`, `AuthFailureMismatch "mismatch"`, `AuthFailureExpired "expired"`, `AuthFailureUsed "used"`. `type ErrorCode string` — `ErrorCodeUnauthorized "unauthorized"`, `ErrorCodeInvalidBody "invalid_body"`, `ErrorCodeInvalidUser "invalid_user"`, `ErrorCodeNotFound "not_found"`, `ErrorCodeInternal "internal_error"`.
  - `errors.go`: `ErrUnauthorized`; `type AuthError struct{ Reason AuthFailureReason }` with `Error() string` (`"access: unauthorized: <reason>"`) and `Unwrap() error` (returns `ErrUnauthorized`); `ErrMalformedCredential`, `ErrInvalidUser`, `ErrInviteNotFound`, `ErrInviteUsed`, `ErrTokenNotFound`.
  - `credential.go`: `type Credential struct{ Prefix, ID, Secret string }`; `func (credential Credential) Encode() string` (`Prefix + ID + "_" + Secret`; `String`/`Format`/`LogValue` render `<prefix><id>_[REDACTED]`; `IssuedInvite` and `IssuedToken` redact the same way); `func ParseCredential(raw, prefix string) (Credential, error)`.
  - `models.go`: `Principal{TokenID, User string}`; `Invite{ID, User string; ExpiresAt time.Time; UsedAt *time.Time; CreatedAt time.Time}`; `StoredInvite{Invite; SecretHash string}`; `Token{ID, User string; CreatedAt time.Time; LastUsedAt *time.Time}`; `StoredToken{Token; SecretHash string}`; `IssuedInvite{Invite Invite; Secret, RedeemCommand string}` and `IssuedToken{Token Token; Secret string}` (`Secret` = the full encoded credential, shown once); `Settings{AdminKeyHash, PublicDomain string}`; `func WithPrincipal(ctx context.Context, principal Principal) context.Context`; `func PrincipalFrom(ctx context.Context) (Principal, bool)`; `func ValidUser(user string) bool`; `func RedeemCommand(publicDomain, invite string) string`.
  - `interfaces.go` (each with its doc comment): ports `InviteStore{Create(ctx, invite StoredInvite) error; Find(ctx, id string) (StoredInvite, error); Redeem(ctx, inviteID string, token StoredToken) error}`, `TokenStore{Find(ctx, id string) (StoredToken, error); List(ctx) ([]Token, error); Delete(ctx, id string) error; TouchLastUsed(ctx, id string, usedAt time.Time) error}`, `SecretSource{NewID() (string, error); NewSecret() (string, error)}`, `Hasher{Hash(secret string) (string, error); Matches(hash, secret string) bool}`, `Clock{Now() time.Time}`; use cases `InviteIssuer{Issue(ctx, user string) (IssuedInvite, error)}`, `InviteRedeemer{Redeem(ctx, rawInvite string) (IssuedToken, error)}`, `TokenAdmin{List(ctx) ([]Token, error); Delete(ctx, id string) error}`, `TokenAuthenticator{Authenticate(ctx, rawToken string) (Principal, error)}`, `AdminAuthenticator{Authenticate(ctx, rawKey string) error}`. Store docs name their sentinels: `Find` → `ErrInviteNotFound`/`ErrTokenNotFound`, `Redeem` → `ErrInviteUsed` (marks the invite used and inserts the token in one transaction), `Delete` → `ErrTokenNotFound`.
- Produces (package `infrastructure`): `NewRandomSecret() *RandomSecret` (`SecretSource`), `NewBcryptHasher() *BcryptHasher` (`Hasher`, cost `bcrypt.DefaultCost`), `NewSystemClock() *SystemClock` (`Clock`, returns `time.Now().UTC()`).

- [ ] **Step 1: Write the failing domain tests**

`internal/access/domain/credential_test.go` (package `domain_test`), fixtures `id := "0123456789abcdef0123456789abcdef"`, `secret := strings.Repeat("A", 43)`:

```go
func TestParseCredential(t *testing.T)
	// valid: {"token", "nct_"+id+"_"+secret, TokenPrefix} → Credential{TokenPrefix, id, secret}
	//        {"secret with underscore and dash", "nct_"+id+"_"+"_-"+strings.Repeat("A", 41), TokenPrefix}
	//        {"invite", "nci_"+id+"_"+secret, InvitePrefix}
	// malformed (assert.ErrorIs(t, err, domain.ErrMalformedCredential)):
	//        invite parsed as token; ""; "nct_"; id one char short; upper-case id; "g" in id;
	//        missing separator ("nct_"+id+secret+"A"); secret 42 chars; secret 44 chars;
	//        "=" padding as last char; "+" as last char; non-canonical last char "B";
	//        trailing "\n"
func TestCredential_EncodeRoundTrips(t *testing.T)
	// ParseCredential(Credential{TokenPrefix, id, secret}.Encode(), TokenPrefix) == the same Credential
func TestEncodedSecretLength_MatchesBase64URL(t *testing.T)
	assert.Equal(t, base64.RawURLEncoding.EncodedLen(domain.SecretBytes), domain.EncodedSecretLength)
```

`internal/access/domain/models_test.go`:

```go
func TestValidUser(t *testing.T)
	// valid: "alice", "a", "alice.smith_2-x", "a"+strings.Repeat("b", 63)
	// invalid: "", "Alice", "-alice", ".alice", "a"+strings.Repeat("b", 64), "alice smith", "älice", "alice\n"
func TestPrincipalFrom_ReturnsStoredPrincipal(t *testing.T)
	// WithPrincipal(ctx, Principal{"t1","alice"}) → (that principal, true)
func TestPrincipalFrom_EmptyContext(t *testing.T)   // → (Principal{}, false)
func TestRedeemCommand(t *testing.T)
	// "notifycat.example.com" → `curl -fsS -X POST -H @- https://notifycat.example.com/api/v1/invites/redeem <<<'Authorization: Invite nci_x'`
	// ""                      → `curl -fsS -X POST -H @- "$NOTIFYCAT_URL/api/v1/invites/redeem" <<<'Authorization: Invite nci_x'`
func TestAuthError_UnwrapsToUnauthorized(t *testing.T)
	assert.ErrorIs(t, &domain.AuthError{Reason: domain.AuthFailureMismatch}, domain.ErrUnauthorized)
	assert.ErrorContains(t, ..., "mismatch")
```

- [ ] **Step 2: Write the failing infrastructure tests** in `internal/access/infrastructure/primitives_test.go`

```go
func TestRandomSecret_NewIDIsSixteenRandomBytesInHex(t *testing.T)
	// len 32, hex.DecodeString → 16 bytes, two calls differ
func TestRandomSecret_NewSecretIsThirtyTwoRandomBytesInBase64URL(t *testing.T)
	// len 43, base64.RawURLEncoding.Strict().DecodeString → 32 bytes, two calls differ
func TestRandomSecret_GeneratedCredentialParses(t *testing.T)
	// ParseCredential(Credential{TokenPrefix, id, secret}.Encode(), TokenPrefix) → no error
func TestBcryptHasher_HashMatchesOnlyItsSecret(t *testing.T)
	cost, err := bcrypt.Cost([]byte(hash)); require.NoError(t, err)
	assert.Equal(t, 10, cost)
	assert.True(t, hasher.Matches(hash, "s3cret"))
	assert.False(t, hasher.Matches(hash, "other"))
	assert.False(t, hasher.Matches("", "s3cret"))
	assert.False(t, hasher.Matches("not-a-hash", "s3cret"))
func TestSystemClock_ReturnsUTC(t *testing.T)
	assert.Equal(t, time.UTC, clock.Now().Location())
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test -race ./internal/access/... -v`
Expected: FAIL to compile — package `internal/access/domain` has no `ParseCredential`, `ValidUser`, …

- [ ] **Step 4: Implement the domain files** listed under Interfaces

`ParseCredential` algorithm (no splitting on `_`): cut `prefix` (else malformed); the rest must be exactly `EncodedIDLength + 1 + EncodedSecretLength` bytes; `rest[:32]` only `[0-9a-f]`; `rest[32] == '_'`; `rest[33:]` must decode with `base64.RawURLEncoding.Strict()` to exactly `SecretBytes` bytes. Every failure returns `ErrMalformedCredential`. `ValidUser` uses a package-level `regexp.MustCompile(UserLabelPattern)`. `RedeemCommand` uses `"https://" + publicDomain + RedeemPath` when the domain is set, else `"\"" + PublicURLVariable + RedeemPath + "\""`. The context key is an unexported struct type.

- [ ] **Step 5: Implement the infrastructure primitives**

`RandomSecret` reads `IDBytes` / `SecretBytes` from `crypto/rand` and encodes with `hex.EncodeToString` / `base64.RawURLEncoding`. `BcryptHasher.Matches` is `bcrypt.CompareHashAndPassword(...) == nil`.

- [ ] **Step 6: Add the depguard rules** `access-domain` (files `**/internal/access/domain/**`; allow kernel and `internal/access/domain`) and `access-application` (files `**/internal/access/application/**`; allow kernel, `internal/access/domain`, `internal/access/application`), same `desc` style as the review rules.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test -race ./internal/access/... -v`
Expected: PASS.

- [ ] **Step 8: Run the task gate.** Expected: exit 0 (depguard accepts the new packages).

- [ ] **Step 9: Commit**

```bash
git add .golangci.yml go.mod go.sum internal/access/
git commit -m "feat(access): add credential model, secret source and bcrypt hasher"
```

---

### Task 3: Migration 00010 and the invite / token repositories

**Files:**
- Create: `internal/platform/persistence/migrations/00010_access_tokens.sql`
- Modify: `internal/platform/persistence/models.go` (`AccessInvite`, `AccessToken`), `internal/platform/persistence/models_test.go` (`TestTableNames`)
- Create: `internal/platform/persistence/access.go`, `internal/platform/persistence/access_test.go`
- Modify: `internal/platform/persistence/code_reviews_test.go:170` (a third `MigrateDown`, commented `// 00010`, before the two existing ones, so `TestCodeReviews_Migration00008DownRestoresSingleActiveIndex` still lands on 00007)
- Create: `internal/access/infrastructure/invite_repo.go`, `internal/access/infrastructure/token_repo.go`, `internal/access/infrastructure/repos_test.go`

**Interfaces:**
- Consumes: `domain.InviteStore`, `domain.TokenStore`, `domain.StoredInvite`, `domain.StoredToken`, `domain.Token`, the domain sentinels (Task 2); `persistence.ErrNotFound`, `persistence.NewTestDB`.
- Produces:
  - `persistence.AccessInvite{ID string (primaryKey); User string; SecretHash string; ExpiresAt time.Time; UsedAt *time.Time; CreatedAt time.Time}` → `access_invites`; `persistence.AccessToken{ID, User, SecretHash string; CreatedAt time.Time; LastUsedAt *time.Time}` → `access_tokens`.
  - `persistence.ErrInviteUsed`.
  - `persistence.NewAccessInvites(db *gorm.DB) *AccessInvites` with `Create(ctx, invite AccessInvite) error`, `Find(ctx, id string) (AccessInvite, error)` (`ErrNotFound`), `Redeem(ctx, inviteID string, token AccessToken) error` — one transaction: `UPDATE access_invites SET used_at = token.CreatedAt WHERE id = ? AND used_at IS NULL`; zero rows → `ErrInviteUsed`; then insert the token.
  - `persistence.NewAccessTokens(db *gorm.DB) *AccessTokens` with `Find(ctx, id string) (AccessToken, error)` (`ErrNotFound`), `List(ctx) ([]AccessToken, error)` (`ORDER BY created_at, id`), `Delete(ctx, id string) error` (zero rows → `ErrNotFound`), `TouchLastUsed(ctx, id string, usedAt time.Time) error`.
  - `infrastructure.NewInviteRepo(invites *persistence.AccessInvites) *InviteRepo` (implements `domain.InviteStore`, maps `ErrNotFound` → `ErrInviteNotFound`, `ErrInviteUsed` → `domain.ErrInviteUsed`); `infrastructure.NewTokenRepo(tokens *persistence.AccessTokens) *TokenRepo` (implements `domain.TokenStore`, maps `ErrNotFound` → `ErrTokenNotFound`; `List` returns `[]domain.Token`, never a hash). Both end with `var _ domain.X = (*Repo)(nil)`.

- [ ] **Step 1: Write the migration** `00010_access_tokens.sql` — create-only, no table rebuild:

```sql
-- +goose Up
CREATE TABLE access_invites (
    id          TEXT     PRIMARY KEY,
    user        TEXT     NOT NULL,
    secret_hash TEXT     NOT NULL,
    expires_at  DATETIME NOT NULL,
    used_at     DATETIME,
    created_at  DATETIME NOT NULL
);

CREATE TABLE access_tokens (
    id           TEXT     PRIMARY KEY,
    user         TEXT     NOT NULL,
    secret_hash  TEXT     NOT NULL,
    created_at   DATETIME NOT NULL,
    last_used_at DATETIME
);

-- +goose Down
DROP TABLE access_tokens;
DROP TABLE access_invites;
```

- [ ] **Step 2: Write the failing persistence tests** in `internal/platform/persistence/access_test.go`

Helper `seedToken(t, db, tokenID string, createdAt time.Time)`: creates invite `"invite-"+tokenID` and redeems it with that token. Compare times with `assert.WithinDuration(..., time.Second)`.

```go
func TestAccessInvites_CreateAndFind(t *testing.T)                  // all six columns round-trip
func TestAccessInvites_Find_UnknownIsErrNotFound(t *testing.T)      // assert.ErrorIs(err, persistence.ErrNotFound)
func TestAccessInvites_Redeem_MarksUsedAndCreatesToken(t *testing.T)
	// invite.UsedAt == token.CreatedAt; AccessTokens.Find(token.ID) has User "alice" and the hash
func TestAccessInvites_Redeem_UsedInviteIsErrInviteUsed(t *testing.T)
	// second Redeem with token "t2" → ErrInviteUsed; Find("t2") → ErrNotFound
func TestAccessInvites_Redeem_RollsBackWhenTokenInsertFails(t *testing.T)
	// seedToken "t1"; Redeem(invite B, token ID "t1") → error; invite B UsedAt stays nil
func TestAccessTokens_ListOrdersByCreatedAt(t *testing.T)           // seeded newer-first, listed older-first
func TestAccessTokens_List_EmptyIsEmpty(t *testing.T)               // assert.Empty
func TestAccessTokens_Delete(t *testing.T)                          // then Find → ErrNotFound; Delete unknown → ErrNotFound
func TestAccessTokens_TouchLastUsed(t *testing.T)                   // LastUsedAt set to usedAt
func TestMigration00010_DownDropsAccessTables(t *testing.T)
	// after MigrateDown: zero sqlite_master rows named access_invites/access_tokens; after MigrateUp: two
```

Extend `TestTableNames` with `access_invites` and `access_tokens`.

- [ ] **Step 3: Write the failing adapter tests** in `internal/access/infrastructure/repos_test.go` (real `persistence.NewTestDB`)

```go
func TestInviteRepo_RoundTripsStoredInvite(t *testing.T)
func TestInviteRepo_FindUnknownIsErrInviteNotFound(t *testing.T)    // assert.ErrorIs(err, domain.ErrInviteNotFound)
func TestInviteRepo_RedeemTwiceIsErrInviteUsed(t *testing.T)        // assert.ErrorIs(err, domain.ErrInviteUsed)
func TestTokenRepo_UnknownIsErrTokenNotFound(t *testing.T)          // Find and Delete both
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test -race ./internal/platform/persistence/ ./internal/access/... -v`
Expected: FAIL to compile — `persistence.NewAccessInvites`, `infrastructure.NewInviteRepo` undefined.

- [ ] **Step 5: Implement** `models.go` additions, `access.go`, `invite_repo.go`, `token_repo.go`, and the `code_reviews_test.go` fix, per Interfaces. `Redeem` uses `r.db.WithContext(ctx).Transaction(...)` and checks `RowsAffected`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -race ./internal/platform/persistence/ ./internal/access/... -v`
Expected: PASS, including `TestCodeReviews_Migration00008DownRestoresSingleActiveIndex`.

- [ ] **Step 7: Run the task gate.** Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
git add internal/platform/persistence/ internal/access/infrastructure/
git commit -m "feat(persistence): add access_invites and access_tokens tables"
```

---

### Task 4: Use cases — invites, token admin, token and admin authentication

**Files:**
- Modify: `internal/access/domain/models.go` (params DTOs)
- Create: `internal/access/application/{invite_issuer.go,invite_redeemer.go,token_admin.go,token_authenticator.go,admin_authenticator.go}`
- Create: `internal/access/application/{fakes_test.go,invite_issuer_test.go,invite_redeemer_test.go,token_admin_test.go,token_authenticator_test.go,admin_authenticator_test.go}`

**Interfaces:**
- Consumes: Task 2 domain (ports, `ParseCredential`, `ValidUser`, `RedeemCommand`, `AuthError`, sentinels, constants).
- Produces:
  - DTOs in `models.go`: `InviteIssuerParams{Invites InviteStore; Secrets SecretSource; Hasher Hasher; Clock Clock; Settings Settings; Logger *slog.Logger}`, `InviteRedeemerParams{Invites InviteStore; Secrets SecretSource; Hasher Hasher; Clock Clock; Logger *slog.Logger}`, `TokenAuthenticatorParams{Tokens TokenStore; Hasher Hasher; Clock Clock; Logger *slog.Logger}`.
  - `application.NewInviteIssuer(params domain.InviteIssuerParams) *InviteIssuer`
  - `application.NewInviteRedeemer(params domain.InviteRedeemerParams) *InviteRedeemer`
  - `application.NewTokenAdmin(tokens domain.TokenStore, logger *slog.Logger) *TokenAdmin`
  - `application.NewTokenAuthenticator(params domain.TokenAuthenticatorParams) *TokenAuthenticator`
  - `application.NewAdminAuthenticator(settings domain.Settings, hasher domain.Hasher, logger *slog.Logger) *AdminAuthenticator`
  - Errors: auth failures are `*domain.AuthError` (so `errors.Is(err, domain.ErrUnauthorized)`); `Issue` returns `domain.ErrInvalidUser`; `TokenAdmin.Delete` returns `domain.ErrTokenNotFound`; store failures are returned wrapped and are never `ErrUnauthorized`.

Behaviour per use case:
- `Issue`: `!ValidUser` → `ErrInvalidUser` (nothing stored) → `NewID`, `NewSecret`, `Hash(secret)` → `Create(StoredInvite{Invite{ID, User, ExpiresAt: now+InviteTTL, CreatedAt: now}, hash})` → `INFO agent invite created user= invite_id= expires_at=` → `IssuedInvite{Invite, Secret: Credential{InvitePrefix, id, secret}.Encode(), RedeemCommand: RedeemCommand(settings.PublicDomain, encoded)}`.
- `Redeem`: parse (`InvitePrefix`) → malformed; `Find` `ErrInviteNotFound` → unknown; `!Matches` → mismatch; `UsedAt != nil` → used; `!now.Before(ExpiresAt)` → expired; then `NewID`, `NewSecret`, `Hash` → `Invites.Redeem(ctx, invite.ID, StoredToken{Token{ID, User: invite.User, CreatedAt: now}, hash})`; `ErrInviteUsed` → used. Every failure logs `WARN agent invite redeem failed reason=<reason>`; success logs `INFO agent token created user= token_id=` and returns `IssuedToken{Token, Secret: Credential{TokenPrefix, id, secret}.Encode()}`.
- `Authenticate` (token): parse (`TokenPrefix`) → malformed; `Find` `ErrTokenNotFound` → unknown; `!Matches` → mismatch; each logs `WARN agent api auth failed reason=<reason>`. On success, when `LastUsedAt == nil || now.Sub(*LastUsedAt) >= LastUsedResolution`, call `TouchLastUsed(ctx, id, now)`; its error logs `WARN agent token last_used_at not updated token_id= err=` and is swallowed. Returns `Principal{TokenID, User}`.
- `TokenAdmin.Delete`: `Find` (→ `ErrTokenNotFound`), `Delete`, then `INFO agent token deleted user= token_id=`. `List` passes through.
- `AdminAuthenticator.Authenticate`: empty key or `!Matches(settings.AdminKeyHash, key)` → `WARN admin auth failed` and `domain.ErrUnauthorized`; the empty key never reaches the hasher.

- [ ] **Step 1: Write the fakes** in `fakes_test.go` (package `application_test`)

`fakeInviteStore` (map by id; records `created []domain.StoredInvite`, `redeemed []redeemCall{inviteID string; token domain.StoredToken}`; settable `findErr`, `redeemErr`), `fakeTokenStore` (map by id; records `touched []touchCall{id string; usedAt time.Time}`, `deleted []string`; settable `findErr`, `touchErr`), `fakeSecrets` (pops queued ids and secrets), `fakeHasher` (`Hash(s)` = `"hash:"+s`; `Matches(h, s)` = `h == "hash:"+s`; counts `hashCalls`, `matchCalls`), `fakeClock{now time.Time}`, `captureLogger() (*slog.Logger, *bytes.Buffer)` (text handler). Fixtures: `now = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)`, `inviteID = "0123456789abcdef0123456789abcdef"`, `tokenID = "fedcba9876543210fedcba9876543210"`, `inviteSecret = strings.Repeat("A", 43)`, `tokenSecret = strings.Repeat("B", 42) + "A"`.

- [ ] **Step 2: Write the failing tests**

```go
// invite_issuer_test.go
func TestInviteIssuer_Issue_StoresHashedInviteForOneHour(t *testing.T)
	require.Len(t, invites.created, 1)
	assert.Equal(t, domain.StoredInvite{Invite: domain.Invite{ID: inviteID, User: "alice", ExpiresAt: now.Add(time.Hour), CreatedAt: now}, SecretHash: "hash:" + inviteSecret}, invites.created[0])
	assert.Equal(t, "nci_"+inviteID+"_"+inviteSecret, issued.Secret)
	assert.Equal(t, now.Add(time.Hour), issued.Invite.ExpiresAt)
func TestInviteIssuer_Issue_RedeemCommandUsesServerDomain(t *testing.T)
	assert.Equal(t, "curl -fsS -X POST -H @- https://notifycat.example.com/api/v1/invites/redeem <<<'Authorization: Invite nci_"+inviteID+"_"+inviteSecret+"'", issued.RedeemCommand)
func TestInviteIssuer_Issue_RejectsInvalidUser(t *testing.T)     // table: "", "Alice", "-alice", 65 chars, "alice smith"
	assert.ErrorIs(t, err, domain.ErrInvalidUser)
	assert.Empty(t, invites.created)
func TestInviteIssuer_Issue_LogsWithoutSecret(t *testing.T)
	assert.Contains(t, logged.String(), "agent invite created")
	assert.Contains(t, logged.String(), "user=alice")
	assert.Contains(t, logged.String(), "invite_id="+inviteID)
	assert.NotContains(t, logged.String(), inviteSecret)

// invite_redeemer_test.go — seeded invite: user alice, CreatedAt now, ExpiresAt now+1h, hash "hash:"+inviteSecret
func TestInviteRedeemer_Redeem_CreatesTokenAndMarksInviteUsed(t *testing.T)
	require.Len(t, invites.redeemed, 1)
	assert.Equal(t, redeemCall{inviteID: inviteID, token: domain.StoredToken{Token: domain.Token{ID: tokenID, User: "alice", CreatedAt: now}, SecretHash: "hash:" + tokenSecret}}, invites.redeemed[0])
	assert.Equal(t, "nct_"+tokenID+"_"+tokenSecret, issued.Secret)
	assert.Contains(t, logged.String(), "agent token created")
func TestInviteRedeemer_Redeem_ValidJustBeforeExpiry(t *testing.T)   // clock = ExpiresAt - time.Nanosecond → no error
func TestInviteRedeemer_Redeem_Failures(t *testing.T)
	// testCases: {name, raw, arrange func, wantReason, wantMatchCalls}
	//   malformed       raw "nci_garbage"                         → AuthFailureMalformed, 0 bcrypt calls
	//   unknown         findErr = domain.ErrInviteNotFound        → AuthFailureUnknown,   0 bcrypt calls
	//   mismatch        stored hash "hash:other"                  → AuthFailureMismatch
	//   used            stored UsedAt = &now                      → AuthFailureUsed
	//   expired         clock = ExpiresAt                         → AuthFailureExpired
	//   expired later   clock = ExpiresAt + time.Second           → AuthFailureExpired
	//   lost race       redeemErr = domain.ErrInviteUsed          → AuthFailureUsed
	var authErr *domain.AuthError
	require.ErrorAs(t, err, &authErr)
	assert.Equal(t, testCase.wantReason, authErr.Reason)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.Contains(t, logged.String(), "agent invite redeem failed")
	assert.Contains(t, logged.String(), "reason="+string(testCase.wantReason))
	// for every case but "lost race": assert.Empty(t, invites.redeemed)
func TestInviteRedeemer_Redeem_StoreErrorIsNotUnauthorized(t *testing.T)  // findErr = errors.New("disk")
	assert.NotErrorIs(t, err, domain.ErrUnauthorized)

// token_authenticator_test.go — seeded token tokenID, user alice, hash "hash:"+tokenSecret
func TestTokenAuthenticator_Authenticate_ReturnsPrincipal(t *testing.T)
	assert.Equal(t, domain.Principal{TokenID: tokenID, User: "alice"}, principal)
func TestTokenAuthenticator_Authenticate_Failures(t *testing.T)
	// malformed ("nct_short", and a valid invite string) → 0 bcrypt calls; unknown → 0 bcrypt calls; mismatch
	// same ErrorAs / ErrorIs / log assertions as the redeemer, message "agent api auth failed"
func TestTokenAuthenticator_LastUsedWrittenAtMostOncePerMinute(t *testing.T)
	// testCases: {"never used", nil, true}, {"30s ago", now-30s, false}, {"59s ago", now-59s, false},
	//            {"60s ago", now-60s, true}, {"2h ago", now-2h, true}
	// wantTouch → assert.Equal(t, []touchCall{{id: tokenID, usedAt: now}}, tokens.touched); else assert.Empty
func TestTokenAuthenticator_TouchFailureStillAuthenticates(t *testing.T)
	require.NoError(t, err)
	assert.Contains(t, logged.String(), "agent token last_used_at not updated")
func TestTokenAuthenticator_StoreErrorIsNotUnauthorized(t *testing.T)

// token_admin_test.go
func TestTokenAdmin_List_ReturnsStoreTokens(t *testing.T)
func TestTokenAdmin_Delete_RemovesAndLogs(t *testing.T)
	assert.Equal(t, []string{tokenID}, tokens.deleted)
	assert.Contains(t, logged.String(), "agent token deleted")
	assert.Contains(t, logged.String(), "user=alice")
func TestTokenAdmin_Delete_UnknownIsErrTokenNotFound(t *testing.T)
	assert.ErrorIs(t, err, domain.ErrTokenNotFound)
	assert.Empty(t, tokens.deleted)

// admin_authenticator_test.go — settings.AdminKeyHash = "hash:nca_right"
func TestAdminAuthenticator_AcceptsTheKey(t *testing.T)
func TestAdminAuthenticator_RejectsWrongKey(t *testing.T)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.Contains(t, logged.String(), "admin auth failed")
	assert.NotContains(t, logged.String(), "nca_wrong")
func TestAdminAuthenticator_EmptyKeySkipsBcrypt(t *testing.T)
	assert.ErrorIs(t, err, domain.ErrUnauthorized)
	assert.Zero(t, hasher.matchCalls)
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test -race ./internal/access/application/ -v`
Expected: FAIL to compile — `application.NewInviteIssuer` and the params DTOs are undefined.

- [ ] **Step 4: Implement** the DTOs and the five use cases per the behaviour list above. Each struct ends with `var _ domain.X = (*Y)(nil)`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -race ./internal/access/application/ -v`
Expected: PASS.

- [ ] **Step 6: Run the task gate.** Expected: exit 0 (depguard: application imports only `access/domain`).

- [ ] **Step 7: Commit**

```bash
git add internal/access/domain/models.go internal/access/application/
git commit -m "feat(access): add invite, token and admin use cases"
```

---

### Task 5: HTTP adapters — JSON helpers, admin, redeem, bearer

**Files:**
- Create: `internal/platform/httpx/json.go`, `internal/platform/httpx/json_test.go`
- Modify: `internal/access/domain/models.go` (`AdminHandlerParams`)
- Create: `internal/access/infrastructure/{authorization.go,bearer_middleware.go,admin_middleware.go,admin_handlers.go,redeem_handler.go}`
- Create: `internal/access/infrastructure/{http_fakes_test.go,bearer_middleware_test.go,admin_handlers_test.go,redeem_handler_test.go}`

**Interfaces:**
- Consumes: the use-case interfaces, `AuthError`, sentinels, `ErrorCode` values, route constants, `WithPrincipal` (Task 2).
- Produces:
  - `httpx.WriteJSON(w http.ResponseWriter, status int, body any)` — sets `Content-Type: application/json` and `Cache-Control: no-store`; `httpx.WriteError(w http.ResponseWriter, status int, code string)` — `{"error": code}`; `httpx.WriteNoContent(w http.ResponseWriter)` — `204` with `Cache-Control: no-store`.
  - `domain.AdminHandlerParams{Authenticator AdminAuthenticator; Invites InviteIssuer; Tokens TokenAdmin; Logger *slog.Logger}`.
  - `infrastructure.BearerMiddleware(authenticator domain.TokenAuthenticator, logger *slog.Logger) func(http.Handler) http.Handler` — success calls next with `domain.WithPrincipal`; `ErrUnauthorized` → `401 {"error":"unauthorized"}` + `WWW-Authenticate: Bearer`; other error → `500 {"error":"internal_error"}` + `ERROR agent api request failed err=`.
  - `infrastructure.AdminMiddleware(authenticator domain.AdminAuthenticator) func(http.Handler) http.Handler` — `401 {"error":"unauthorized"}` + `WWW-Authenticate: Bearer`.
  - `infrastructure.NewAdminHandler(params domain.AdminHandlerParams) http.Handler` — a mux with `CreateInvitePattern`, `ListTokensPattern`, `DeleteTokenPattern`, the whole mux wrapped in `AdminMiddleware`. Responses: invite `201 {"invite","user","expires_at","redeem_command"}`; tokens `200 {"tokens":[{"id","user","created_at","last_used_at"}]}` (`last_used_at` `null` until used; empty list `[]`); delete `204`, unknown `404 {"error":"not_found"}`; body over `MaxRequestBodyBytes` or bad JSON `400 invalid_body`; `ErrInvalidUser` `400 invalid_user`; other errors `500 internal_error` + `ERROR admin request failed err=`.
  - `infrastructure.NewRedeemHandler(redeemer domain.InviteRedeemer, logger *slog.Logger) http.Handler` — `201 {"user","token"}`; `ErrUnauthorized` → `401 {"error":"unauthorized"}` + `WWW-Authenticate: Invite`; other error → `500` + `ERROR agent api request failed err=`. The body is ignored.
  - Unexported `credentialFromHeader(request *http.Request, scheme string) string`: splits `Authorization` at the first space, `strings.EqualFold` on the scheme, `strings.TrimSpace` on the rest; `""` when absent or another scheme. The middlewares and the redeem handler pass that string (possibly empty) to the use case, which logs the failure reason.

- [ ] **Step 1: Write the failing tests**

```go
// httpx/json_test.go
func TestWriteJSON_SetsStatusAndHeaders(t *testing.T)    // 201, application/json, no-store, JSONEq body
func TestWriteError_WritesErrorCode(t *testing.T)        // assert.JSONEq(t, `{"error":"unauthorized"}`, body)
func TestWriteNoContent(t *testing.T)                    // 204, no-store, empty body

// access/infrastructure — fakes in http_fakes_test.go: fakeTokenAuthenticator{principal, err, gotRaw},
// fakeAdminAuthenticator{err, calls}, fakeInviteIssuer{issued, err, gotUser}, fakeTokenAdmin{tokens, err, deleted},
// fakeInviteRedeemer{issued, err, gotRaw}; a validToken fixture "nct_"+id+"_"+secret.

// bearer_middleware_test.go
func TestBearerMiddleware_PutsPrincipalInContext(t *testing.T)
	// next handler: principal, ok := domain.PrincipalFrom(r.Context()) — assert only
	assert.Equal(t, domain.Principal{TokenID: "t1", User: "alice"}, gotPrincipal)
	assert.Equal(t, validToken, authenticator.gotRaw)
func TestBearerMiddleware_AcceptsHeaderVariants(t *testing.T)
	// "bearer "+validToken, "Bearer  "+validToken+"\t", "BEARER "+validToken → authenticator.gotRaw == validToken
func TestBearerMiddleware_EveryFailureIsTheSame401(t *testing.T)
	// testCases: no header; "Basic abc"; "Bearer "; "Invite "+validToken;
	//            authenticator err = &domain.AuthError{Reason: AuthFailureUnknown}; ... AuthFailureMismatch
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
	assert.Equal(t, "Bearer", recorder.Header().Get("WWW-Authenticate"))
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	assert.False(t, nextCalled)
func TestBearerMiddleware_AuthenticatorErrorIs500(t *testing.T)   // errors.New("disk") → 500 internal_error

// admin_handlers_test.go
func TestAdminHandler_CreateInvite(t *testing.T)
	// POST /admin/v1/invites {"user":"alice"}, Authorization: Bearer nca_key
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"invite":"nci_x","user":"alice","expires_at":"2026-10-28T10:00:00Z","redeem_command":"curl …"}`, body)
	assert.Equal(t, "alice", issuer.gotUser)
func TestAdminHandler_CreateInvite_BadRequests(t *testing.T)
	// {"not json", "invalid_body"}, {strings.Repeat("x", 16<<10+1), "invalid_body"},
	// {`{"user":"Alice"}` with issuer err domain.ErrInvalidUser, "invalid_user"} → 400 + JSONEq error body
func TestAdminHandler_ListTokens(t *testing.T)
	assert.JSONEq(t, `{"tokens":[{"id":"t1","user":"alice","created_at":"2026-10-28T09:00:00Z","last_used_at":null},
	                             {"id":"t2","user":"bob","created_at":"2026-10-28T09:05:00Z","last_used_at":"2026-10-28T09:30:00Z"}]}`, body)
func TestAdminHandler_ListTokens_EmptyIsEmptyArray(t *testing.T)   // JSONEq `{"tokens":[]}`
func TestAdminHandler_DeleteToken(t *testing.T)                    // 204; tokens.deleted == []string{"t1"}
func TestAdminHandler_DeleteToken_UnknownIs404(t *testing.T)        // err ErrTokenNotFound → 404 `{"error":"not_found"}`
func TestAdminHandler_RequiresAdminKey(t *testing.T)
	// fake admin authenticator returns domain.ErrUnauthorized; testCases over
	// POST /admin/v1/invites, GET /admin/v1/tokens, DELETE /admin/v1/tokens/t1, GET /admin/v1/unknown
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	assert.JSONEq(t, `{"error":"unauthorized"}`, recorder.Body.String())
	assert.Equal(t, "Bearer", recorder.Header().Get("WWW-Authenticate"))
	assert.Empty(t, issuer.gotUser, "no admin use case runs without the key")

// redeem_handler_test.go
func TestRedeemHandler_ReturnsToken(t *testing.T)
	// POST /api/v1/invites/redeem, Authorization: Invite nci_x
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"user":"alice","token":"nct_y"}`, recorder.Body.String())
	assert.Equal(t, "nci_x", redeemer.gotRaw)
	assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
func TestRedeemHandler_EveryFailureIsTheSame401(t *testing.T)
	// no header; "Bearer nci_x" (gotRaw == ""); redeemer errors AuthFailureUsed / AuthFailureExpired
	// → 401, `{"error":"unauthorized"}`, WWW-Authenticate "Invite"
func TestRedeemHandler_StoreErrorIs500(t *testing.T)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -race ./internal/platform/httpx/ ./internal/access/infrastructure/ -v`
Expected: FAIL to compile — `httpx.WriteJSON`, `infrastructure.BearerMiddleware`, `NewAdminHandler`, `NewRedeemHandler` undefined.

- [ ] **Step 3: Implement** the files per Interfaces. Response bodies are unexported structs with `json` tags; times go through `.UTC().Format(time.RFC3339)`; `last_used_at` is a `*string`. `POST /admin/v1/invites` reads through `http.MaxBytesReader(w, r.Body, domain.MaxRequestBodyBytes)`. Status-to-code mapping uses the `ErrorCode` constants.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/platform/httpx/ ./internal/access/infrastructure/ -v`
Expected: PASS.

- [ ] **Step 5: Run the task gate.** Expected: exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/httpx/ internal/access/
git commit -m "feat(access): add admin, redeem and bearer HTTP adapters"
```

---

### Task 6: `access` fx module and runtime wiring

**Files:**
- Create: `internal/access/module.go`, `internal/access/module_test.go`
- Create: `internal/platform/httpx/route.go`
- Create: `internal/runtime/agent_api.go`, `internal/runtime/agent_api_test.go`
- Modify: `internal/runtime/module.go` (`Module`, `buildMux`, `buildServer`, `registerServer`)

**Interfaces:**
- Consumes: everything above; `persistence.NewAccessInvites`, `persistence.NewAccessTokens` (Task 3); `config.Config` fields (Task 1).
- Produces:
  - `access.Module` — `fx.Module("access", fx.Provide(...))` binding `NewInviteRepo`→`InviteStore`, `NewTokenRepo`→`TokenStore`, `NewRandomSecret`→`SecretSource`, `NewBcryptHasher`→`Hasher`, `NewSystemClock`→`Clock`, and the five use cases to their interfaces (fx.In param structs build the params DTOs). Expects `*persistence.AccessInvites`, `*persistence.AccessTokens`, `accessdomain.Settings`, `*slog.Logger`.
  - `httpx.Route{Pattern string; Handler http.Handler}`.
  - `runtime.AsAgentAPIRoutes(constructor any) any` = `fx.Annotate(constructor, fx.ResultTags(`group:"agent_api_routes,flatten"`))`; the constructor returns `[]httpx.Route`.
  - `runtime.AdminServer struct{ *http.Server }` — provided as `*AdminServer`, nil unless `cfg.AgentAPIEnabled && cfg.AdminAddr != ""`.
  - Unexported in `agent_api.go`: `provideAccessSettings(cfg config.Config) accessdomain.Settings` (`AdminKeyHash: cfg.AdminKeyHash.Reveal()`, `PublicDomain: cfg.Domain`); `agentAPIInputs` (fx.In: `Redeemer accessdomain.InviteRedeemer`, `Authenticator accessdomain.TokenAuthenticator`, `BearerRoutes []httpx.Route \`group:"agent_api_routes"\``); `registerAgentAPI(mux *http.ServeMux, cfg config.Config, inputs agentAPIInputs, logger *slog.Logger)` — returns early when disabled, else registers `accessdomain.RedeemPattern` → `NewRedeemHandler` and every group route as `mux.Handle(route.Pattern, BearerMiddleware(...)(route.Handler))`; `adminServerInputs` + `buildAdminServer(inputs adminServerInputs) *AdminServer`; `registerAdminServer(lifecycle fx.Lifecycle, adminServer *AdminServer, cfg config.Config, logger *slog.Logger, shutdowner fx.Shutdowner)` — nil server + `cfg.AdminAddr != ""` → `WARN admin server not started reason="agent_api.enabled is false"`.
  - In `module.go`: `newHardenedServer(addr string, handler http.Handler) *http.Server` (today's `buildServer` timeouts; `buildServer` delegates to it); `serveInLifecycle(lifecycle, server *http.Server, listeningMessage string, logger, shutdowner)` (today's `registerServer` body, message as parameter; `registerServer` passes `"listening"`, `registerAdminServer` passes `"admin server listening"`); `buildMux` gains a trailing `agentAPI agentAPIInputs` parameter and calls `registerAgentAPI`; `Module` gains `access.Module`, `persistence.NewAccessInvites`, `persistence.NewAccessTokens`, `provideAccessSettings`, `buildAdminServer`, and `fx.Invoke(registerAdminServer)` after `registerServer`.

- [ ] **Step 1: Write the failing module test** `internal/access/module_test.go` (template: `internal/review/module_test.go`)

```go
func TestModule_GraphResolves(t *testing.T)
	// fxtest.New(t, access.Module, fx.Provide(NewAccessInvites(db), NewAccessTokens(db), accessdomain.Settings{}, discard logger),
	//   fx.Invoke(func(domain.InviteIssuer, domain.InviteRedeemer, domain.TokenAdmin, domain.TokenAuthenticator, domain.AdminAuthenticator) {}))
	app.RequireStart()
	app.RequireStop()
```

- [ ] **Step 2: Write the failing runtime tests** in `internal/runtime/agent_api_test.go` (package `runtime_test`; reuses `newTestConfig` and `request` from `wire_test.go`)

Helpers: `const testAdminKey = "nca_" + 43 base64url chars`; `agentAPIConfig(t) config.Config` = `newTestConfig(t)` plus `AgentAPIEnabled: true`, `AdminAddr: "127.0.0.1:9091"`, `AdminKeyHash: config.Secret(bcrypt(testAdminKey, bcrypt.MinCost))`, `Domain: "notifycat.example.com"`; `probeRoutes() []httpx.Route` → `GET /api/v1/probe` writing `{"user": principal.User}` (assert only inside the handler); `buildAgentAPIApp(t, cfg, options ...fx.Option) (public *httptest.Server, admin *runtime.AdminServer)` = `fx.New(fx.Supply(cfg), runtime.Module, fx.NopLogger, fx.Provide(runtime.AsAgentAPIRoutes(probeRoutes)), options..., fx.Populate(&server, &adminServer, &db))` with the DB closed in `t.Cleanup`; `freeLoopbackAddr(t) string`.

```go
func TestAgentAPI_DisabledServesNoRoutesAndNoAdminServer(t *testing.T)
	// agentAPIConfig with AgentAPIEnabled false
	assert.Equal(t, http.StatusNotFound, redeemStatus)   // POST /api/v1/invites/redeem
	assert.Equal(t, http.StatusNotFound, probeStatus)    // GET /api/v1/probe with a Bearer header
	assert.Nil(t, adminServer)
func TestAgentAPI_EnabledRegistersRedeemAndBearerRoutes(t *testing.T)
	assert.Equal(t, http.StatusUnauthorized, redeemStatus)   // no Authorization header
	assert.Equal(t, http.StatusUnauthorized, probeStatus)
func TestAgentAPI_AdminRoutesAreNotOnThePublicMux(t *testing.T)
	// table over POST /admin/v1/invites, GET /admin/v1/tokens, DELETE /admin/v1/tokens/t1, each with Bearer testAdminKey
	assert.Equal(t, http.StatusNotFound, status)
func TestAgentAPI_NoAdminServerWithoutAdminAddr(t *testing.T)    // AdminAddr "" → assert.Nil(t, adminServer)
func TestAgentAPI_AdminServerUsesHardenedTimeouts(t *testing.T)
	assert.Equal(t, "127.0.0.1:9091", adminServer.Addr)
	assert.Equal(t, 5*time.Second, adminServer.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, adminServer.ReadTimeout)
	assert.Equal(t, 15*time.Second, adminServer.WriteTimeout)
	assert.Equal(t, 60*time.Second, adminServer.IdleTimeout)
	assert.Equal(t, 1<<14, adminServer.MaxHeaderBytes)
func TestModule_ServesAdminServerOnSecondListener(t *testing.T)
	// fxtest.New with Addr "127.0.0.1:0", AdminAddr freeLoopbackAddr(t); RequireStart
	require.Eventually(t, func() bool { /* GET http://<admin>/admin/v1/tokens == 401 */ }, 5*time.Second, 50*time.Millisecond)
	// RequireStop
func TestModule_AdminServerBindFailureExitsOne(t *testing.T)
	// hold a net.Listener on 127.0.0.1:0, AdminAddr = its address; fx.New(...); require.NoError(t, app.Start(ctx))
	// select on app.Wait() with a 5s timeout:
	assert.Equal(t, 1, signal.ExitCode)
	// require.NoError(t, app.Stop(ctx))
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test -race ./internal/access/ ./internal/runtime/ -run 'TestModule_|TestAgentAPI_' -v`
Expected: FAIL to compile — `access.Module`, `runtime.AsAgentAPIRoutes`, `runtime.AdminServer`, `httpx.Route` undefined.

- [ ] **Step 4: Implement** `internal/access/module.go`, `internal/platform/httpx/route.go`, `internal/runtime/agent_api.go`, and the `module.go` changes per Interfaces. Update the `Module` doc comment in `runtime/module.go` to mention the admin server.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -race ./internal/access/ ./internal/runtime/ -v`
Expected: PASS — the new tests and every existing runtime test (`TestModule_StartsAndStops`, `TestWire_*`, `TestIntegration_*`) unchanged.

- [ ] **Step 6: Run the task gate.** Expected: exit 0.

- [ ] **Step 7: Commit**

```bash
git add internal/access/module.go internal/access/module_test.go internal/platform/httpx/route.go internal/runtime/
git commit -m "feat(runtime): serve the admin port and the agent API routes"
```

---

### Task 7: `notifycat-keygen` and the Dockerfile

**Files:**
- Create: `cmd/notifycat-keygen/main.go`, `cmd/notifycat-keygen/main_test.go`
- Modify: `Dockerfile` (one `go build` line and one `COPY` line, aligned with the others), `justfile` (recipe `keygen:` → `go run ./cmd/notifycat-keygen`), `docs/cli.md`

**Interfaces:**
- Consumes: `accessdomain.SecretSource`, `accessdomain.Hasher`, `accessdomain.AdminKeyPrefix` (Task 2); `accessinfra.NewRandomSecret`, `accessinfra.NewBcryptHasher`.
- Produces: `run(stdout, stderr io.Writer, secrets accessdomain.SecretSource, hasher accessdomain.Hasher) int`. `main` calls `os.Exit(run(os.Stdout, os.Stderr, accessinfra.NewRandomSecret(), accessinfra.NewBcryptHasher()))` and reads no config. Output: stdout `nca_<secret>\n`; stderr `NOTIFYCAT_ADMIN_KEY_HASH='<bcrypt hash>'\n`. Any error → stderr `notifycat-keygen: <err>`, exit 1, nothing on stdout.

- [ ] **Step 1: Write the failing tests**

```go
func TestRun_PrintsKeyAndMatchingHash(t *testing.T)
	// real infrastructure
	require.Equal(t, 0, code)
	key := strings.TrimSuffix(stdout.String(), "\n")
	assert.Regexp(t, `^nca_[A-Za-z0-9_-]{43}$`, key)
	match := regexp.MustCompile(`^NOTIFYCAT_ADMIN_KEY_HASH='(\$2a\$10\$[./A-Za-z0-9]{53})'\n$`).FindStringSubmatch(stderr.String())
	require.Len(t, match, 2)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(match[1]), []byte(key)), "the printed hash verifies the printed key")
func TestRun_SecretFailureExitsOne(t *testing.T)
	// fake SecretSource whose NewSecret returns errors.New("no entropy")
	assert.Equal(t, 1, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "notifycat-keygen:")
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -race ./cmd/notifycat-keygen/ -v`
Expected: FAIL — package `cmd/notifycat-keygen` has no Go files / `run` undefined.

- [ ] **Step 3: Implement** `cmd/notifycat-keygen/main.go` (package doc: usage, and why the quotes matter), the Dockerfile lines, and the `justfile` recipe.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./cmd/notifycat-keygen/ -v`
Expected: PASS.

- [ ] **Step 5: Verify the image**

Run: `docker build -t notifycat:test . && docker run --rm notifycat:test notifycat-keygen`
Expected: an `nca_…` line on stdout and the `NOTIFYCAT_ADMIN_KEY_HASH='$2a$10$…'` line on stderr. If the bare name is not found in the scratch image, use `/usr/local/bin/notifycat-keygen` in `docs/cli.md` and `docs/security.md` instead.

- [ ] **Step 6: Docs** — `docs/cli.md`: add the `notifycat-keygen` row to the table, fix the binary count in the intro to match the table, and add a `## notifycat-keygen` section (what it prints where, the single quotes, `docker run --rm ghcr.io/mptooling/notifycat notifycat-keygen`, reads no config, run it on the admin's machine and keep the key in a password manager).

- [ ] **Step 7: Run the task gate.** Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
git add cmd/notifycat-keygen/ Dockerfile justfile docs/cli.md
git commit -m "feat(keygen): add notifycat-keygen for the admin key"
```

---

### Task 8: Secret-leak test, end-to-end runtime test, docs

**Files:**
- Create: `internal/access/secret_leak_test.go`
- Create: `internal/runtime/integration_agent_api_test.go`
- Modify: `docs/security.md`, `docs/operations.md`, `ARCHITECTURE.md`, `CLAUDE.md`, `README.md`

**Interfaces:**
- Consumes: `access.Module`, the HTTP adapters (Task 5), `runtime.AsAgentAPIRoutes`, `runtime.AdminServer`, and the Task 6 test helpers `agentAPIConfig`, `buildAgentAPIApp`, `probeRoutes`, `testAdminKey`.
- Produces: tests and docs only.

- [ ] **Step 1: Write the secret-leak test** `internal/access/secret_leak_test.go` (package `access_test`)

Arrange: `logged bytes.Buffer` behind `slog.NewJSONHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})`; an admin key and its `bcrypt.MinCost` hash; the graph from `access.Module` with `persistence.NewTestDB`, `Settings{AdminKeyHash: hash, PublicDomain: "notifycat.example.com"}` and that logger, populated into the five use cases; handlers built with `NewAdminHandler`, `NewRedeemHandler`, and `BearerMiddleware` around a 200 handler. Act (one helper, `runAccessFlow`, collecting every credential it sends or receives): create an invite with the admin key; one admin call with a wrong admin key; redeem the invite; redeem it again; a bearer call with the token; a bearer call with an unknown token; a bearer call with the right id and a wrong secret; a bearer call with a malformed header.

```go
func TestAccess_NeverLogsCredentials(t *testing.T)
	for _, message := range []string{"agent invite created", "agent token created", "agent invite redeem failed",
		"agent api auth failed", "admin auth failed"} {
		assert.Contains(t, logged.String(), message, "the flow really logged")
	}
	// every credential the flow saw, its secret part on its own, and the header prefixes:
	for _, secret := range append(flow.credentials, "Bearer ", "Invite nci_", "$2a$") {
		assert.NotContains(t, logged.String(), secret)
	}
```

- [ ] **Step 2: Write the end-to-end test** `internal/runtime/integration_agent_api_test.go`

```go
func TestIntegration_AgentAccessFlow(t *testing.T)
	// public, adminServer := buildAgentAPIApp(t, agentAPIConfig(t)); admin := httptest.NewServer(adminServer.Handler)
	// 1. POST admin /admin/v1/invites {"user":"alice"} (Bearer testAdminKey) → 201;
	//    redeem_command contains "https://notifycat.example.com/api/v1/invites/redeem"
	// 2. POST public /api/v1/invites/redeem (Invite <invite>) → 201, user "alice", token has prefix "nct_"
	// 3. the same redeem again → 401
	// 4. GET public /api/v1/probe (Bearer <token>) → 200, JSONEq `{"user":"alice"}`
	// 5. GET admin /admin/v1/tokens → one token, user "alice", last_used_at not null
	// 6. DELETE admin /admin/v1/tokens/<id> → 204
	// 7. GET public /api/v1/probe (Bearer <token>) → 401
	// require at each step (each depends on the previous one); assert on the final 401
```

- [ ] **Step 3: Run both tests**

Run: `go test -race ./internal/access/ ./internal/runtime/ -run 'NeverLogsCredentials|AgentAccessFlow' -v`
Expected: PASS (the code exists since Task 6). If either fails, the bug is in Tasks 4–6: fix it there with a regression test in that task's test file, not by loosening these tests.

- [ ] **Step 4: Commit the tests**

```bash
git add internal/access/secret_leak_test.go internal/runtime/integration_agent_api_test.go
git commit -m "test(access): pin credential-free logs and the admin-to-bearer flow"
```

- [ ] **Step 5: Docs**

- `docs/security.md`: new section `## Agent API access` (anchor `#agent-api-access`): the invite → token model (one-hour single-use invite, the admin never sees the token, bcrypt hashes only in SQLite, never in git or `config.yaml`, no end date, deleting the row ends access at once); the admin key (`notifycat-keygen`, only its hash in `NOTIFYCAT_ADMIN_KEY_HASH`, single quotes); the admin port (the spec's three-row table — Compose `127.0.0.1:9091` + `ssh -L`, Kubernetes `kubectl port-forward`, Fly.io `fly-local-6pn:9091` + `fly proxy` — plain HTTP, never publish it, the tunnel encrypts); the public API is HTTPS-only through the operator's proxy; `curl` examples for create invite, list tokens, delete token (header from stdin, as in the spec's agent setup); "What a stolen token gets" and "What a stolen invite gets" from the spec's Security rules. Add two checklist items (admin port not published; `NOTIFYCAT_ADMIN_KEY_HASH` single-quoted).
- `docs/operations.md`: the admin server is a second listener started and stopped with the public one; a fatal admin-server error exits 1. Add a log table: `admin server listening`, `admin server not started`, `admin auth failed`, `agent invite created`, `agent token created`, `agent invite redeem failed` (reasons `malformed|unknown|mismatch|used|expired`), `agent api auth failed` (reasons `malformed|unknown|mismatch`), `agent token deleted`, `agent token last_used_at not updated`, `agent api request failed`, `admin request failed`; state that no line carries a credential or an `Authorization` header.
- `ARCHITECTURE.md`: rewrite "Authentication and authorization" — `internal/access` now owns agent authentication (admin key, invites, tokens) while webhook HMAC stays in `platform/security`; "Seven domains" → "Eight", add an `access` node and table row to the domain map; add `access` to the `notifycat-server` row and a `notifycat-keygen` row (access infrastructure only) to the binary table; mention the JSON helpers and `Route` under `platform/httpx`.
- `CLAUDE.md`: "Seven domains" → "Eight" with an `access` row; the composition-root paragraph names the admin server and the seven CLI binaries (adds `keygen`); the request-flow block gets `POST /admin/v1/*` (admin port) and `POST /api/v1/invites/redeem` lines.
- `README.md`: one feature bullet — admins invite developers with a one-hour single-use invite; each developer redeems it for their own token; admin routes live on a private port — linking `docs/security.md#agent-api-access`.

- [ ] **Step 6: Run the final verification**

Run the task gate, then:
`docker run --rm -v "$PWD:/src" -w /src golang:1.27.3-alpine sh -c 'export PATH=/usr/local/go/bin:$PATH; go install golang.org/x/vuln/cmd/govulncheck@v1.3.0 && /go/bin/govulncheck ./...'`
Expected: every command exits 0; govulncheck reports no vulnerabilities (the new `golang.org/x/crypto` is the latest release).

- [ ] **Step 7: Commit the docs**

```bash
git add docs/security.md docs/operations.md ARCHITECTURE.md CLAUDE.md README.md
git commit -m "docs(access): document the admin port, invites and agent tokens"
```
