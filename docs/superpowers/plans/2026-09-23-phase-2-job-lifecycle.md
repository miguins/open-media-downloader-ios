# Phase 2 — Job Lifecycle Implementation Plan

**Goal:** Deliver authenticated job endpoints, a persistent single-worker queue with a fake extractor, temporary download tokens, a streaming download endpoint, cleanup and startup recovery, and `omdi jobs list|delete|purge`.

**Spec:** `docs/superpowers/specs/2026-09-23-phase-2-job-lifecycle-design.md`

## Global Constraints

- TDD: write the failing test, watch it fail, implement, watch it pass.
- Run every Go command through Compose (`docker compose run --rm --no-deps tools ...` or `make` targets).
- `internal/auth`, `internal/urlpolicy`, and `internal/storage` require 100% coverage; the `internal/...` total stays at least 90%.
- Paths are built only from validated IDs. Extractor output is untrusted.
- Errors and logs never contain secrets, tokens, submitted URLs, paths, or raw extractor output.
- The database is the only cancellation channel between the API, the CLI, and the worker.
- No commit or push without explicit authorization.

## File Map

- `internal/config`: `OMDI_PUBLIC_URL`, `OMDI_MAX_QUEUED_JOBS`, retention-exceeds-timeout rule.
- `internal/store/jobs.go`: claim, complete (transactional), active count, list, delete, purge, expiry, running-job recovery, token replacement.
- `internal/auth/token.go`: download-token generation, parsing, and hashing.
- `internal/storage`: layout, directory creation, ingestion, removal, orphan listing.
- `internal/extractor`: `Request`, `File`, and `Fake`.
- `internal/worker`: `Extractor` interface, claim loop, execution, database cancellation watch.
- `internal/cleanup`: startup recovery and periodic sweep.
- `internal/app`: background loops beside the HTTP server.
- `internal/api`: job and download handlers, JSON helpers, write-deadline extension.
- `cmd/omdi`: wiring and `jobs` subcommands.
- `docs/openapi.yaml`, `collection/`: job and download contracts.
- `Makefile`, `scripts/check-coverage.sh`, `README.md`, `AGENTS.md`, `docs/architecture.md`, `docs/roadmap.md`.

## Tasks

- [x] 1. Configuration additions with tests.
- [x] 2. Store queries with tests, including cross-owner and race cases.
- [x] 3. Download tokens in `internal/auth` at 100% coverage.
- [x] 4. `internal/storage` at 100% coverage.
- [x] 5. `internal/extractor` fake and `internal/worker` with stub-extractor tests.
- [x] 6. `internal/cleanup`, startup recovery, and `internal/app` background loops.
- [x] 7. Job endpoints, OpenAPI, and Bruno.
- [x] 8. Download endpoint, OpenAPI, and Bruno.
- [x] 9. `omdi jobs list|delete|purge` and Make targets.
- [x] 10. Coverage gate, documentation, roadmap, and `make ci`.
