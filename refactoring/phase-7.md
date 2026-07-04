# Phase 7 — diagnostics domain

Operator tooling: preflight `doctor`, `notifycat-config` (list/validate), and `notifycat-smoke` delivery. Absorbs `internal/{doctor,mappingcli,smoke}`, backing three binaries. Cross-cutting readers over routing + validation — safest to migrate last.

Target: `internal/diagnostics/{domain,application,infrastructure}`.

## Layer assignment (pragmatic hexagonal — thick infra, clean application)

The friction (config monolith, `routinginfra.Lock` file I/O, inline `security.Sign`/HTTP) is pushed into **infrastructure**; the **application** orchestrates over domain ports only (no config/store/slack/http/routinginfra/security imports). DTOs reuse `validationdomain.{CheckResult,Report,Status*}` and `routingdomain.Entry`.

### diagnostics/domain — imports stdlib + kernel + `validation/domain` + `routing/domain`
- `models.go`: `Section{Name string; Checks []validationdomain.CheckResult}` + `OK()`; `ConfigSnapshot{DatabaseOpenable bool; DatabaseDetail string; ConfigFile, Domain string; MessageTTLDays int; WebhookSecretSet, SlackTokenSet, GitHubTokenSet bool}` (the *facts* the doctor validates — never raw secrets); `SmokeParams`/`SmokeResult`/`ReactionCheck` (mirror smoke.Result). `LockPlan{ToValidate []routingdomain.Entry; Stale []string}`.
- `interfaces.go`: ports — `DatabaseProbe` (`Probe(dsn string) (ok bool, detail string)`), `EntrySource` (`Entries() []routingdomain.Entry`, `HasPathRules() bool`), `LockGateway` (`Plan(entries []routingdomain.Entry, force bool) (LockPlan, error)`, `Commit(results []validationdomain.EntryResult) error` — wraps the routinginfra lock dance), `Signer` (`Sign(secret string, body []byte) (header, value string)`), `WebhookSender` (`Send(ctx, url string, body []byte, headers map[string]string) (status int, err error)`), `SmokeMappings`/`SmokeMessages`/`SmokeReactions` (the existing smoke consumer ports, over routingdomain/store-free DTOs). Reuse `validationdomain.RepoValidator`/`OrgRepoLister`. Use-case interfaces: `Doctor` (`Run(ctx, target) []Section`), `ConfigCLI` (`List(stdout) int`, `Validate(ctx, target, force, stdout, stderr) int`), `Smoke` (`Run(ctx, target, withReactions) (SmokeResult, error)`).
- `constants.go`: check names, the smoke sentinel errors (`ErrNoMapping`/`ErrSignatureRejected`/`ErrUnreachable`/`ErrUnexpectedStatus`), webhook path.

### diagnostics/application — imports stdlib + diagnostics/domain + validation/{domain,application} + routing/domain
- doctor: `Doctor.Run` assembles config/database/mappings `Section`s (pure rules over `ConfigSnapshot` + `EntrySource`), delegates per-repo probing to `validationdomain.RepoValidator`. `checkConfig`/`checkDatabase`/`checkMappings` become pure functions over ports/DTOs.
- config CLI: `List` (tabulate `EntrySource.Entries()`), `Validate` (targeted/full) driving `LockGateway` + `validationapp.RunForEntries` + the render helpers.
- smoke: `Smoke.Run` sequences the lifecycle, forges the payload (`buildPayload` is pure), signs via `Signer`, sends via `WebhookSender`, reads `SmokeMessages`, verifies via `SmokeReactions`. Sentinel-error mapping stays here.
- Move all three test suites; assertions unchanged (fakes already exist for most ports).

### diagnostics/infrastructure — no depguard rule (imports config/store/slack/http/security/routinginfra)
- `config_snapshot.go`: builds `ConfigSnapshot` from `config.Config` (+ `DatabaseProbe` over `store.Open`/`SQLDB`).
- `lock_gateway.go`: `LockGateway` over `routinginfra.{ReadLock,DiffEntries,MergeLock,WriteLock}`.
- `signer.go`: `Signer` over `security.Sign` + `security.SignatureHeader`.
- `webhook_sender.go`: `WebhookSender` over `*http.Client`.
- `report_writer.go`: `WriteReport(w, sections) bool` (plain-text render + pass/fail aggregate).
- smoke adapters: `SmokeMappings`/`SmokeMessages` over `store`, `SmokeReactions` over `slack.Client`. Report writer + probe tests move here.

### diagnostics/module.go + fx module
- Provide the infra adapters bound to their ports + the three use cases. fxtest builds the graph.

## Sub-steps (coarser — tooling, well-tested)
- **G7-T1** scaffold 3 layers + module stub + 2 depguard rules (diagnostics-domain allows kernel+validation/domain+routing/domain; diagnostics-application also allows validation/application + diagnostics/application) · `refactor(diagnostics): scaffold domain/application/infrastructure`
- **G7-T2** domain (models, interfaces, constants) · `refactor(diagnostics): add domain ports and DTOs`
- **G7-T3** application (doctor + config CLI + smoke use cases + moved tests) · `refactor(diagnostics): relocate doctor, config CLI, smoke use cases`
- **G7-T4** infrastructure (config snapshot/probe, lock gateway, signer, webhook sender, report writer, smoke adapters + tests) · `refactor(diagnostics): relocate report writer and probing adapters`
- **G7-T5** module + fxtest; rewire `cmd/notifycat-{doctor,config,smoke}`; delete `internal/{doctor,mappingcli,smoke}` · `refactor(diagnostics): add fx module and rewire binaries`

## Gate
- [x] build + race tests + lint green (unchanged) · `doctor`/`mappingcli`/`smoke` gone · module fxtested · all three CLIs behave identically · depguard probe-verified · plan box checked

**Done.** 6 commits `3474347`→`4d84ef4`. Executed as **vertical slices** (doctor, config-CLI, smoke) rather than horizontal layers — more coherent for three independent tools; the depguard boundary + moved tests forced correct layering. Race suite 30 test-pkgs, 0 FAIL; lint clean; depguard probe-verified (store blocked in application). Application stays clean: config→`ConfigSnapshot` DTO (infra-built), lock I/O→`LockGateway` port over routinginfra, `security.Sign`/http→`Signer`/`WebhookSender` ports, store/slack→smoke adapter ports. All three CLIs rewired to diagnostics; `diagnostics.Module` fxtests all three use cases.
