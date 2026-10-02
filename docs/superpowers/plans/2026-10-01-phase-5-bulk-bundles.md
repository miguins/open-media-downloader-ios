# Phase 5 — Bulk Bundle Downloads Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task, according to the user's selected execution method. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a succeeded multi-item job expose one safe, persisted ZIP bundle through a short-lived token URL.

**Architecture:** The existing single worker ingests media, removes extraction leftovers, builds a bounded ZIP, and atomically persists job success with item and bundle metadata. Authenticated polling rotates a separate bundle token; GET/HEAD serve a seekable private file. Existing cleanup and recovery own the bundle lifecycle.

**Tech Stack:** Existing pinned Go toolchain, standard-library ZIP and HTTP serving, SQLite through the existing driver, chi routing, Docker Compose, and Bruno. No new dependency or runtime tool.

**Spec:** `docs/superpowers/specs/2026-10-01-phase-5-bulk-bundles-design.md`

## Global Constraints

- Read the approved specification and `docs/roadmap.md`; resolve differences in favor of the specification and project instructions.
- Use `Bundle`, `BundleToken`, `bundle`, `/v1/bundles/{token}`, `bundles`, and `bundle_tokens`; ZIP names the file format.
- Keep English code, comments, documentation, and API messages. Keep existing package boundaries and concrete dependencies.
- Implement behavior with TDD. Use Makefile targets in Docker Compose and prefix shell commands with `rtk`.
- Maintain at least 90% unit coverage and 100% coverage in `internal/auth`, `internal/logging`, `internal/storage`, `internal/urlpolicy`, and `internal/extractor`.
- Store entries without compression. Use `H(n) = 1024 * (n + 1)` bytes and the exact disk/byte/item budgets in the specification.
- Keep directories `0700`, files `0600`, opaque token hashes only, and logs free of credentials, source URLs, internal paths, and raw diagnostics.
- Newly completed multi-item jobs require a bundle; single-item and legacy succeeded jobs omit it. Never build or backfill bundles in HTTP handlers.
- Keep the existing token semantics after API-key revocation; deletion and key purge invalidate bundle tokens through cascading deletion.
- Update the OpenAPI version to `0.5.0`. Every changed endpoint updates HTTP tests, OpenAPI, and organized Bruno requests in the same task.
- Do not commit or push without explicit user authorization. Every authorized commit ends with exactly one `Co-Authored-By: codex <codex@openai.com>` trailer.
- This plan's approval and commit authorization, plus execution-method selection, precede implementation. Specification commit authorization does not authorize committing this plan or future code.
- Run `make ci` before implementation handoff when Docker is available and restore a previously running local service if checks stopped it.

## Review Focus

- Polling through an `OMDI_PUBLIC_URL` path prefix must produce usable bundle URLs; Task 5 tests the existing `/base` fixture.
- Concurrent token rotation must leave exactly one current bundle token without crossing owners; Task 1 tests competing replacements.
- A HEAD request with a Range header must return full-file headers and no body, and HEAD errors must never contain JSON bytes; Task 6 tests both.
- Disk space disappearing after successful admission must fail the job and remove partial output; Tasks 2 and 3 inject a write failure after admission.
- A request opened just before job deletion may finish reading its existing file descriptor, but a subsequent request must return `404`; Task 6 pins this boundary.

## File Map

- Domain/persistence: create `internal/job/bundle.go`, `internal/job/bundle_test.go`, `internal/store/migrations/0003_bundles.sql`, `internal/store/bundles.go`, `internal/store/bundles_test.go`; modify `internal/store/lifecycle.go`, `internal/store/store_test.go`, `internal/store/lifecycle_test.go`, and completion call sites in tests and worker.
- Storage: create `internal/storage/bundles.go`, `internal/storage/bundles_test.go`; modify `internal/storage/storage.go`, its tests, and ingestion call sites to pass context.
- Worker: create `internal/worker/bundles_test.go`; modify `internal/worker/worker.go`, `internal/worker/worker_test.go`, and `internal/worker/logging_test.go`.
- Cleanup/operator verification: modify `internal/cleanup/cleanup.go`, `internal/cleanup/cleanup_test.go`, `internal/cleanup/logging_test.go`; add `cmd/omdi/bundles_test.go` for existing CLI removal flows.
- HTTP: create `internal/api/bundle_links.go`, `internal/api/bundles.go`, `internal/api/bundles_test.go`; modify `internal/api/jobs.go`, `internal/api/jobs_test.go`, `internal/api/router.go`, `internal/api/trace_test.go`, `internal/auth/middleware.go`, `internal/auth/middleware_test.go`.
- Fake/contracts: modify `internal/extractor/extractor.go`, `internal/extractor/fake_test.go`, `docs/openapi.yaml`, and existing collection assertions; create `collection/bundles/{folder,jobs,get-job,get-bundle,head-bundle}.yml`, `collection/bundles/partial/{folder,get-bundle}.yml`, `collection/bundles/unsatisfiable/{folder,get-bundle}.yml`, and `collection/downloads/unsatisfiable/{folder,get-download}.yml`. Create `collection/jobs/unauthorized/{folder,get-job}.yml` if no equivalent assertion request exists.
- Documentation: modify `README.md`, `SECURITY.md`, `AGENTS.md`, `docs/architecture.md`, `docs/self-hosting.md`, `docs/shortcut.md`, and `docs/roadmap.md`.

## Verification and Commit Convention

For each task below, run the new behavioral tests before implementation and confirm
that they fail for the intended missing behavior. Run `rtk make test` after the
implementation and require PASS, with no race failures. Run `rtk make fmt`,
`rtk make coverage`, and `rtk make lint` at each completed code task; require the
coverage thresholds and lint checks to pass. Preserve small synthetic fixtures.

Each task has a suggested atomic commit title. Prepare and inspect its diff after
verification; create the commit only if the user has authorized implementation
commits, with the required trailer. Otherwise keep the verified changes for
review. Do not introduce substitute host-tool commands or unapproved commits.

---

### Task 1: Bundle domain, migration, completion, and token repositories

**Files:** Domain/persistence files in the file map, plus affected completion call sites and `docs/roadmap.md` (`In progress` when execution starts).

**Interfaces:**
- Produce `job.Bundle{JobID string, FileName string, SizeBytes int64, CreatedAt time.Time}` and `job.BundleToken{Hash []byte, JobID string, OwnerID string, CreatedAt time.Time, ExpiresAt time.Time}`.
- Change `(*store.Store).CompleteJob(ctx context.Context, j job.Job, items []job.Item, bundle *job.Bundle) error`.
- Produce `(*store.Store).Bundle(ctx context.Context, ownerID, jobID string) (job.Bundle, error)`.
- Produce `(*store.Store).ReplaceBundleToken(ctx context.Context, token job.BundleToken) error` and `ValidBundleToken(ctx context.Context, hash []byte, now time.Time) (job.BundleToken, error)`.
- Produce `(*store.Store).DeleteExpiredBundleTokens(ctx context.Context, now time.Time) (int64, error)`.

- [ ] Write `TestBundleMigrationPreservesExistingData`: construct schema version 2 with a key, succeeded multi-item job, items, and a live item token; open through the new store and assert version 3, unchanged data/token access, and `Bundle` returns `ErrNotFound`.
- [ ] Write `TestCompleteJobWithBundle`: assert success/items/bundle commit together; nil bundle for two items, bundle for one item, mismatched job IDs, nonpositive size, canceled/deleted job, and duplicate item position reject or roll back without publishing metadata.
- [ ] Write `TestBundleTokenLifecycle`: assert owner-scoped reads, wrong-owner replacement rejection, replacement rollback preserving the previous token on failure, one token after concurrent replacements, exact expiry rejection, job-expiry rejection even when token expiry is later, and cascade deletion through job/key purge. Closed-store operations return safe errors.
- [ ] Run `rtk make test`; confirm failure for the absent bundle interfaces and migration.
- [ ] Implement the types, strict tables, positive-size constraint, cascading foreign keys, unique bundle-token job ID, expiry index, and repository interfaces. Token queries join jobs and bundles and require succeeded, retained jobs with matching owners; revocation alone does not revoke existing tokens.
- [ ] Extend transactional completion to validate item membership and the required optional-bundle relationship, then insert items/bundle alongside the running-to-succeeded transition. Preserve `ErrConflict`/`ErrNotFound` precedence for nonrunning/deleted jobs. Update existing one-item completion callers with `nil` and ordinary multi-item completion tests with valid bundle metadata; construct legacy multi-item fixtures using the preexisting low-level fixture operations rather than weakening new completion validation.
- [ ] Run the task verification checks; inspect rollback and migration results. Suggested commit: `feat(store): persist job bundles and temporary tokens`.

### Task 2: Context-aware ingestion and safe bounded ZIP storage

**Files:** Storage files and ingestion call sites in the file map.

**Interfaces:**
- Change `(*storage.Layout).Ingest(ctx context.Context, jobID string, outputs []storage.Output, maxBytes int64, now time.Time) ([]job.Item, error)`.
- Produce `storage.BundleOverhead(itemCount int) int64`, implementing `1024 * (itemCount + 1)` for validated counts.
- Produce `(*storage.Layout).BuildBundle(ctx context.Context, jobID string, items []job.Item, maxItems int, maxBytes int64, now time.Time) (job.Bundle, error)`.
- Produce `(*storage.Layout).OpenBundle(jobID string, expectedSize int64) (*os.File, error)`.
- Reuse `ErrInvalidID`, `ErrInvalidOutput`, and `ErrTooLarge`; preserve context errors for cancellation/timeout classification.

- [ ] Write `TestBuildBundleMixedMedia`: ingest a video and image, supply items out of order, build/read ZIP, and assert ordered names, store method, original bytes, size metadata, `0600` ZIP, `0700` directory, and download name `omdi-<job-prefix>.zip`.
- [ ] Write `TestBuildBundleRejectsUnsafeInputs`: missing/foreign/duplicate item IDs, duplicate positions/names, traversal, backslash/separator/control-character names, names over 128 bytes, symlink, hard link, directory, zero/truncated/grown source, unsupported metadata, and item/byte limits must fail safely. Ensure rejection cannot replace an existing installed file.
- [ ] Write `TestBuildBundleFailureRemovesTemporaryOutput` and `TestIngestCancellation`: canceled contexts, bounded-copy interruption, partial writes, ZIP finalization, file close, and rename errors leave no published bundle or temp file and disclose no paths. Use narrow package-private I/O helpers at genuine writer/file-operation boundaries for fault injection; never export test switches.
- [ ] Write `TestBundleWriterBudgetAndZIP64`: capped writer permits exactly the byte limit, rejects excess including central-directory writes, and propagates short writes. Exercise ZIP64 header/descriptor encoding with synthetic metadata and a counting/discard writer, without multi-gigabyte allocation or weakening production validation. Assert the conservative overhead bound with longest permitted names.
- [ ] Write `TestOpenBundleRejectsUnsafeFiles`: invalid job ID/size, absent file, symlink, hard link, nonregular file, and recorded-size mismatch reject; valid file opens and reads correctly.
- [ ] Run `rtk make test`; confirm the intended missing-context/bundle behavior failures.
- [ ] Implement context checks throughout ingestion and bounded ZIP copying with a fixed 32 KiB buffer. Validate and sort a copy of the item metadata, confirm source size/link count before and after copying, and copy exactly recorded bytes. Use standard ZIP store encoding, bounded metadata, and item creation timestamps.
- [ ] Exclusively create a `0600` temporary ZIP inside the private job directory; cap output at media bytes plus `BundleOverhead(n)`, close/finalize, verify actual size, and install as `bundle.zip` without overwriting existing output. `OpenBundle` uses the fixed name, no-follow opening, regular/single-link checks, and expected size.
- [ ] Run task verification with 100% storage coverage. Suggested commit: `feat(storage): build safe bounded media bundles`.

### Task 3: Worker bundle lifecycle and disk admission

**Files:** Worker files in the file map.

**Interfaces:**
- Consume Tasks 1–2; retain the public `worker.New` constructor and existing `Settings` fields.
- Change private completion to `(*Worker).complete(lifecycleCtx, jobCtx context.Context, j job.Job, files []extractor.File)`.
- Add private function fields matching `Layout.AvailableBytes` and `Layout.BuildBundle`, initialized to concrete layout methods in `New`, only to exercise admission/failure/cancellation deterministically.

- [ ] Write `TestWorkerCompletesMixedMediaBundle`: two items produce a readable bundle and succeeded metadata; one item produces no bundle; extraction leftovers are removed before the bundle builder runs.
- [ ] Write `TestWorkerBundleDiskAdmission`: with `B = 1 << 20`, `N = 20`, assert initial admission requires `MinFreeBytes + 2*B + 21504`; exact threshold succeeds and one byte below fails. Before building two items totaling 7 bytes, assert the threshold is `MinFreeBytes + 7 + 3072`.
- [ ] Write `TestWorkerBundleFailures`: item-count overflow, storage limits, unsafe input, work removal error, post-admission write failure, and database completion failure assert the specified error code and no success/partial files. Item limits also apply to substituted extractors; do not trust only the real adapter.
- [ ] Write `TestWorkerBundleCancellationAndShutdown`: block the test builder on channels, then trigger timeout, cancellation, purge/deletion, or process shutdown; assert timely cancellation, no succeeded metadata, and correct shutdown recovery state. Check cancellation immediately before database completion and its running-status guard.
- [ ] Run `rtk make test`; confirm multi-item jobs lack the required bundle and admission/cancellation cases fail.
- [ ] Carry both contexts through ingestion, work cleanup, build, and completion. Perform exact disk checks, use the job context for bounded work and completion, and use the live lifecycle context for failure reporting/discard. Preserve extraction retry policy; do not retry bundle creation.
- [ ] Extend fixed-reason worker logs with bundle size/duration where useful and test for token/path/source-URL absence. Run task verification. Suggested commit: `feat(worker): complete multi-item jobs with persisted bundles`.

### Task 4: Recovery, retention, and operator removal

**Files:** Cleanup and operator tests in the file map.

**Interfaces:**
- Consume Task 1 token expiry deletion and existing `Store.Jobs(ctx, store.JobFilter)` / `Layout.RemoveJob(jobID)`.
- Keep `Cleaner.Recover(ctx) error`, periodic cleanup API, and operator command signatures unchanged.

- [ ] Write `TestRecoverRemovesInterruptedBundleFiles`: seed running, failed, and canceled directories with partial/final ZIPs; recovery marks running failed, removes their directories, and preserves queued and succeeded directories, including legacy succeeded media. Repeat recovery to prove idempotency.
- [ ] Write `TestRecoverBundleCleanupFailure`: database/list/removal failures return an error and prevent startup continuation; logs omit paths.
- [ ] Write `TestSweepBundleRetention`: expired bundle tokens disappear while retained ZIPs remain; expired jobs remove items, ZIPs, metadata, and both token types; fresh jobs remain. A failed file removal leaves the row for retry and reports a fixed error.
- [ ] Write `TestOperatorRemovesBundles`: exercise existing job delete/purge and revoked-key purge with real storage/store fixtures, asserting directory and token deletion; revoke without purge keeps already-issued tokens valid.
- [ ] Run `rtk make test`; confirm missing recovery and expiry handling fail.
- [ ] Extend startup recovery to remove failed/canceled job directories after marking interrupted jobs failed and clearing work. Extend periodic token cleanup to both tables, with safe counts/errors. Reuse entire-directory removal in operator flows.
- [ ] Run task verification. Suggested commit: `feat(cleanup): recover and expire bundle artifacts`.

### Task 5: Polling bundle links and multi-item collection fixture

**Files:** `internal/api/jobs.go`, new `bundle_links.go`, API tests, fake extractor/tests, `docs/openapi.yaml`, `collection/bundles/folder.yml`, `jobs.yml`, `get-job.yml`, and existing single-item polling assertions.

**Interfaces:**
- Add `Bundle *bundleResponse` with `json:"bundle,omitempty"` to `jobResponse`.
- Define `bundleResponse` with `file_name`, `media_type`, `size_bytes`, `download_url`, and `download_expires_at`, using the existing response field types and no ID.
- Produce `(*jobHandler).bundleLink(request *http.Request, j job.Job) (*bundleResponse, error)` consuming owner-scoped metadata and token replacement from Task 1.
- Fake multi-item fixture URL: `https://www.instagram.com/p/DduKfFmDxsG/`, matching the approved collection example; return one synthetic `video/mp4` and one `image/jpeg`. Other URLs retain their one-item fixture.

- [ ] Write `TestSucceededJobIssuesBundleLink`: use `newAPIFixture`, genuine two-item completion and ZIP storage; assert the exact five-field schema, media type, size/name, `/base/v1/bundles/` URL prefix, independent token rotation, and expiry capped by job expiry. Other-owner polling returns `404` without issuing tokens.
- [ ] Write `TestJobOmitsBundle`: single-item, queued, running, failed, canceled, and legacy succeeded multi-item jobs omit the field. Write `TestBundleLinkIssuanceFailure`: metadata/token storage failure yields safe no-store `500`, not a silently omitted bundle.
- [ ] Extend fake tests for the exact fixture URL, two media types, aggregate bytes, max-items rejection, context cancellation, and safe write errors. Assert its fixture selection does not appear in the real extractor composition.
- [ ] Run `rtk make test`; confirm missing response/fixture behavior fails.
- [ ] Implement the response/link interfaces, reuse opaque-token hashing/generation, and cap expiry with the same polling timestamp convention as items. Distinguish missing legacy metadata from repository failure. Extend Fake only; add no production request switch.
- [ ] Update OpenAPI Job/Bundle schemas and polling descriptions/examples, set version `0.5.0`, and add organized collection create/poll requests using the fixed fixture. Keep separate runtime variables `bundleJobId`, `bundlePollAttempts`, and `bundleToken`; bound polling to 20 attempts with 500 ms pauses. Existing polling asserts single-item jobs omit `bundle`.
- [ ] Run task verification and `rtk make collection-test`; require the existing single-item and new mixed-item polling flows to pass. Suggested commit: `feat(api): expose temporary bundle links when polling jobs`.

### Task 6: Token-only bundle GET/HEAD and complete endpoint contract

**Files:** New `internal/api/bundles.go`, `bundles_test.go`; router/trace tests; OpenAPI and bundle GET/HEAD collection requests.

**Interfaces:**
- Produce `bundleHandler{deps Dependencies}` with `serve(response http.ResponseWriter, request *http.Request)` and fixed-reason rejection handling.
- Register GET and HEAD `/v1/bundles/{token}` outside API-key middleware.
- Consume `Store.ValidBundleToken`, owner-scoped `Store.Bundle`, `Layout.OpenBundle`, and existing rolling-deadline writer.

- [ ] Write `TestBundleDownloadAndHead`: GET bytes parse as the expected mixed ZIP; HEAD has the same full-file headers/length and zero body, including when Range is supplied. Valid requests work without Authorization and reuse one token for HEAD/GET/ranges.
- [ ] Write `TestBundleDownloadRanges`: satisfiable single/multiple ranges return correct bytes and status; unsatisfiable ranges return `416`, `Content-Range: bytes */<size>`, and safe headers. Check last-modified conditional behavior against the existing item content-serving convention and document actual statuses.
- [ ] Write `TestBundleDownloadUniformErrors`: malformed, unknown, replaced, expired, wrong-route, deleted, and missing/unsafe/size-mismatched files all produce `404`; HEAD errors contain zero bytes. Item tokens cannot download bundles, and bundle tokens cannot download items.
- [ ] Write `TestBundleDownloadDeletionBoundary`: once the handler has opened a descriptor, delete the job/files; the established stream can finish, and every subsequent request returns `404`. Use channels to coordinate rather than sleeps.
- [ ] Extend trace tests to assert `/v1/bundles/{token}` route patterns and no token, raw URL, internal path, or filesystem diagnostic leakage on successes and failures.
- [ ] Run `rtk make test`; confirm missing route/HEAD behavior fails.
- [ ] Implement the token/metadata/file checks and safe headers, serve through the existing content/deadline behavior, and strip Range from HEAD using a cloned request. Ensure rejection on HEAD writes headers/status only. Retain uniform download errors even for repository failures.
- [ ] Add OpenAPI GET/HEAD paths, Bundle responses/headers, no API-key requirement, reusable-token/replacement descriptions, and actual conditional/range statuses. Create Bruno `get-bundle`/`head-bundle` assertions for content type, disposition, sizes, trace/privacy headers, and no HEAD body. In the `partial` and `unsatisfiable` subfolders, add `get-bundle` requests using `Range: bytes=0-3` and a range starting at the recorded bundle size, asserting `206` and `416` respectively. Polling stores `bundleSize` for those assertions; endpoint credentials remain token-only.
- [ ] Run task verification and `rtk make collection-test`; require the whole HTTP flow to pass. Suggested commit: `feat(api): serve ZIP bundles through temporary tokens`.

### Task 7: Authentication cache and item range contract corrections

**Files:** Auth middleware/tests, API download tests, OpenAPI Unauthorized/item range responses, corresponding collection assertions/requests.

**Interfaces:** Keep public APIs unchanged; the private auth JSON writer sets `Cache-Control: no-store` before status.

- [ ] Extend `TestMiddlewareRejectsInvalidCredentials` and `TestMiddlewareStoreFailure` to assert `Cache-Control: no-store` for `401` and `500`, with unchanged safe JSON bodies and Bearer challenge semantics.
- [ ] Write `TestItemDownloadUnsatisfiableRange`: with a known 5-byte file and `Range: bytes=5-`, assert `416`, `Content-Range: bytes */5`, no-store/privacy headers, and actual content-server error type/body. Do not describe that response as JSON unless it is JSON.
- [ ] Run `rtk make test`; confirm the missing authentication header fails while the item range regression pins current behavior.
- [ ] Add the no-store header to the auth JSON writer and preserve safe download headers through the actual content-server error path. Add OpenAPI Unauthorized cache header and item GET `416` response. Add a no-Authorization `get-job` under `collection/jobs/unauthorized/` with `401`/no-store assertions, and `get-download` under `collection/downloads/unsatisfiable/` with a range starting at the recorded item size and `416`/Content-Range/no-store assertions. Existing polling stores `downloadSize`; pin OpenAPI/auth `500` declarations consistently.
- [ ] Run task verification and `rtk make collection-test`. Suggested commit: `fix(api): declare download ranges and prevent caching auth errors`.

### Task 8: User documentation and operational guidance

**Files:** Documentation files in the file map.

**Interfaces:** Describe the shipped API and limits from Tasks 1–7; no product code changes.

- [ ] Update README and `docs/shortcut.md` with mixed-media polling, choosing `bundle.download_url` for one ZIP, token rotation/expiry, individual alternatives, and legacy-job behavior. Use synthetic examples with no credentials or real media.
- [ ] Update architecture/security/self-hosting notes with worker ZIP generation, token isolation, ZIP store format, recovery behavior, exact admission formulas, the lack of a filesystem reservation, and approximately doubled retained media disk usage.
- [ ] Update `AGENTS.md` current scope and link the approved spec/plan in the roadmap; leave Phase 5 `In progress` until Task 9 passes.
- [ ] Verify cross-links, API names, version examples, and that docs describe ZIP as the format and bundle as the collection. Run `rtk git diff --check`. Suggested commit: `docs: explain bundle downloads and capacity requirements`.

### Task 9: Whole-change verification and phase closure

**Files:** Final corrections only where evidence requires; `docs/roadmap.md`.

**Interfaces:** No new interfaces; confirm the complete specification against delivered behavior.

- [ ] Inspect the final diff against every acceptance criterion, including migration, ZIP64/overhead, fault paths, owner isolation, mixed media, deletion races, recovery, contract synchronization, and log privacy. Perform the review required by the selected execution workflow; fix findings and rerun the affected checks.
- [ ] Record whether the local Compose service is running before verification. Run `rtk make ci`; require every target to pass, including race tests, coverage, lint, scans, builds, production smoke, and the complete offline collection. Do not claim real-platform archive extraction was tested unless an explicitly selected real smoke fixture was exercised.
- [ ] Restore the local service with `rtk make compose-up-detached` if it was running before checks. Mark Phase 5 `Complete` only after all required checks pass; state there is no next phase in the current roadmap rather than inventing Phase 6.
- [ ] Run `rtk git diff --check`, inspect the final status, and report verification evidence and material limits. Prepare the remaining diff/commit for user review under the existing authorization policy. Suggested closure commit: `docs: mark phase 5 bundle downloads complete`.

## Self-Review Record

The tasks cover each approved-spec section. All downstream interfaces are defined
above their consumers. The five review-focus cases have owning tests. Migration
compatibility uses legacy fixtures without weakening new completion rules;
cancelable work and lifecycle failure reporting use separate contexts; HTTP
tasks update their executable and documented contracts together. Implementation
has not started; plan approval, commit authorization, and execution-method
selection remain the next gates.
