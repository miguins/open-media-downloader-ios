# Phase 2 — Job Lifecycle Delivery Record

> **Historical record:** This document was consolidated at the Phase 2 handoff after implementation. It records delivered scope and verification; it is not a pre-implementation plan in the current `superpowers:writing-plans` format and must not be used as a template for future phases.

**Delivered outcome:** Authenticated job endpoints, a persistent single-worker queue with a fake extractor, temporary download tokens, a streaming download endpoint, cleanup and startup recovery, and `omdi jobs list|delete|purge`.

**Design:** `docs/superpowers/specs/2026-09-23-phase-2-job-lifecycle-design.md`

## Delivery Constraints

- TDD: write the failing test, watch it fail, implement, watch it pass.
- Run every Go command through Compose (`docker compose run --rm --no-deps tools ...` or `make` targets).
- `internal/auth`, `internal/urlpolicy`, and `internal/storage` require 100% coverage; the `internal/...` total stays at least 90%.
- Paths are built only from validated IDs. Extractor output is untrusted.
- Errors and logs never contain secrets, tokens, submitted URLs, paths, or raw extractor output.
- The database is the only cancellation channel between the API, the CLI, and the worker.
- No commit or push without explicit authorization.

## Delivered File Map

- `internal/config`: `OMDI_PUBLIC_URL`, `OMDI_MAX_QUEUED_JOBS`, and the retention-exceeds-timeout rule.
- `internal/store/jobs.go`: claim, transactional completion, active count, list, delete, purge, expiry, running-job recovery, and token replacement.
- `internal/auth/token.go`: download-token generation, parsing, and hashing.
- `internal/storage`: layout, directory creation, ingestion, removal, and orphan listing.
- `internal/extractor`: `Request`, `File`, and `Fake`.
- `internal/worker`: `Extractor` interface, claim loop, execution, and database cancellation watch.
- `internal/cleanup`: startup recovery and periodic sweep.
- `internal/app`: background loops beside the HTTP server.
- `internal/api`: job and download handlers, JSON helpers, and write-deadline extension.
- `cmd/omdi`: wiring and `jobs` subcommands.
- `docs/openapi.yaml`, `collection/`: job and download contracts.
- `Makefile`, `scripts/check-coverage.sh`, `README.md`, `AGENTS.md`, `docs/architecture.md`, `docs/roadmap.md`.

## Delivered Work

- [x] Configuration additions and cross-field validation.
- [x] Queue, lifecycle, owner-isolation, race, expiry, and token-replacement store operations.
- [x] Opaque download tokens in `internal/auth` at 100% coverage.
- [x] Private output ingestion and filesystem defenses in `internal/storage` at 100% coverage.
- [x] Fake extractor and single worker with success, failure, timeout, cancellation, race, deletion, and budget tests.
- [x] Startup recovery, periodic cleanup, and application background loops.
- [x] Authenticated job endpoints with synchronized tests, OpenAPI, and Bruno requests.
- [x] Token-based download endpoint with `Range`, `HEAD`, and synchronized contracts.
- [x] `omdi jobs list|delete|purge` and matching Make targets.
- [x] Coverage gate, documentation, roadmap status, and the complete CI suite.
