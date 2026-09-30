# Roadmap

This document is the source of truth for implementation order and delivery status. [Architecture](architecture.md) describes the target system; it does not change the order below or imply that planned components already exist.

Status values are `Complete`, `In progress`, and `Planned`. A phase must be complete before implementation begins on the next phase.

## Current position

**Phase 4.1 — Releases and published images is complete. Phase 5 is next.**

The next development effort is **Phase 5 — Bulk archive downloads**.

The Phase 1 and Phase 2 documents under `docs/superpowers/records/` were consolidated at their handoffs and record delivered scope; they are not pre-implementation plans under the current Superpowers format. Phase 3 and later phases follow the complete current workflow before implementation begins.

## Phase 0 — Project foundation

**Status: Complete**

- Go module and `omdi serve` executable structure.
- Minimal validated `OMDI_HTTP_ADDR` configuration.
- Structured JSON logging and signal-aware HTTP server lifecycle.
- Unauthenticated `GET /healthz` liveness endpoint.
- Compose-first local development and pinned media-tool runtime.
- Formatting, race tests, coverage, lint, vulnerability, secret, build, image, smoke, and collection checks.
- OpenAPI contract and organized Bruno collection for `/healthz`.
- Public README, architecture, security policy, repository instructions, and MIT license.

## Phase 1 — Secure core

**Status: Complete** — [design](superpowers/specs/2026-09-22-phase-1-secure-core-design.md), [delivery record](superpowers/records/2026-09-22-phase-1-secure-core.md)

- Complete application configuration for persistence, storage, resource limits, security, and extractor paths.
- SQLite migrations and repositories.
- API-key CLI and authentication.
- Job, item, and token domain models.
- Public-media URL normalization, allowlisting, and SSRF defenses.
- Readiness endpoint backed by real dependency checks.

Structured logging and liveness established in Phase 0 remain cross-cutting requirements. New Phase 1 flows must preserve secret redaction, safe errors, and the security invariants in [Architecture](architecture.md).

## Phase 2 — Job lifecycle

**Status: Complete** — [design](superpowers/specs/2026-09-23-phase-2-job-lifecycle-design.md), [delivery record](superpowers/records/2026-09-23-phase-2-job-lifecycle.md)

- Create, poll, and cancel endpoints.
- Persistent queue and single worker.
- Fake extractor.
- Temporary token generation.
- Streaming download endpoint.
- Cleanup and startup recovery.
- Operator job management: `omdi jobs list`, `delete`, and `purge`.

## Phase 3 — Real extractors

**Status: Complete** — [design](superpowers/specs/2026-09-23-phase-3-real-extractors-design.md), [delivery record](superpowers/records/2026-09-23-phase-3-real-extractors.md)

- `yt-dlp` adapter.
- `ffprobe` metadata inspection.
- FFmpeg merge/remux behavior.
- `gallery-dl` adapter.
- Platform and error normalization.
- Optional real smoke-test command.

## Phase 4 — Packaging and documentation finalization

**Status: Complete** — [design](superpowers/specs/2026-09-29-phase-4-packaging-documentation-design.md), [plan](superpowers/plans/2026-09-29-phase-4-packaging-documentation.md)

- Finalize Dockerfile and Compose for the complete runtime.
- Complete the OpenAPI specification for every implemented endpoint.
- Validate and update the README local quick start for the completed system.
- Add the Shortcut construction guide.
- Finalize CI coverage for the complete feature set.
- Finalize security and self-hosting notes.

Phase 0 provides working baselines for several of these deliverables. Phase 4 completes them against the finished feature set; those baselines do not make Phase 4 active.

## Phase 4.1 — Releases and published images

**Status: Complete** — [design](superpowers/specs/2026-09-30-phase-4-1-releases-design.md), [plan](superpowers/plans/2026-09-30-phase-4-1-releases.md)

A continuation of Phase 4 packaging that publishes each version for self-hosters who do not want to build the image.

- A release workflow triggered by a semantic version tag that matches the OpenAPI version.
- A passing CI run for the tagged commit on `main` before anything is published.
- A multi-architecture runtime image for `linux/amd64` and `linux/arm64`, built on native runners and published to the GitHub Container Registry under the exact version, never a floating tag.
- A signed build provenance attestation for the published image.
- A GitHub release with generated notes and the image reference and digest.
- Workflow linting in CI, and documentation of prebuilt-image deployment and release verification.

`compose.yaml` keeps building from source; the published image is a documented alternative.

## Phase 5 — Bulk archive downloads

**Status: Planned**

A job with more than one item, such as a carousel, can be downloaded as one ZIP archive instead of one request per item.

- Direction: a succeeded multi-item job exposes an `archive` object beside `items`, with its own opaque, short-lived download token and expiry, for example `GET /v1/archives/{token}` (Bruno request `get-archive`). The archive keeps the existing token-as-credential model of `/v1/downloads/{token}`.
- A job ID is never accepted as an unauthenticated download credential. Job IDs are identifiers that appear in API responses, operator commands, and logs, do not expire, and would bypass ownership isolation.
- Rejected alternative: an authenticated `GET /v1/jobs/{id}/archive`. It is safe, but the link cannot be handed to Safari or a Shortcut file action without the `Authorization` header.
- Open questions for the design specification: build the archive on demand or persist it until cleanup (on-demand saves disk but complicates `Content-Length` and `Range`); reuse `/v1/downloads/{token}` or add a dedicated route; archive naming.
- Archive entries use the ZIP store method because media is already compressed. The archive is bounded by the existing `OMDI_MAX_JOB_ITEMS` and `OMDI_MAX_JOB_BYTES` limits plus a fixed per-entry overhead.
- Contract corrections found during Phase 4:
  - Authentication `401` and `500` responses must send `Cache-Control: no-store` like every other JSON response, and the OpenAPI `Unauthorized` response must declare it.
  - `GET /v1/downloads/{token}` returns `416 Range Not Satisfiable` with `Content-Range: bytes */<size>` for an unsatisfiable `Range`; the OpenAPI contract must declare it, with a test pinning the behavior.

## Delivery rules

- Use the current Superpowers workflow for every new phase: approve and commit the design specification, approve and commit a current-format implementation plan, and select the execution method before implementation begins.
- Implement behavior with TDD and the coverage requirements in `AGENTS.md`.
- Update this document in the same change whenever a phase changes status or order.
- Do not create placeholder packages or speculative interfaces for later phases.
