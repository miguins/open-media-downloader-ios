# Roadmap

This document is the source of truth for implementation order and delivery status. [Architecture](architecture.md) describes the target system; it does not change the order below or imply that planned components already exist.

Status values are `Complete`, `In progress`, and `Planned`. A phase must be complete before implementation begins on the next phase.

## Current position

**Phase 0 — Project foundation is complete. Phase 1 has not started.**

The next development effort is a Superpowers design and implementation plan for **Phase 1 — Secure core**, beginning with the complete configuration contract required by persistence, authentication, URL security, and readiness checks.

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

**Status: Planned — next**

- Complete application configuration for persistence, storage, resource limits, security, and extractor paths.
- SQLite migrations and repositories.
- API-key CLI and authentication.
- Job, item, and token domain models.
- Public-media URL normalization, allowlisting, and SSRF defenses.
- Readiness endpoint backed by real dependency checks.

Structured logging and liveness established in Phase 0 remain cross-cutting requirements. New Phase 1 flows must preserve secret redaction, safe errors, and the security invariants in [Architecture](architecture.md).

## Phase 2 — Job lifecycle

**Status: Planned**

- Create, poll, and cancel endpoints.
- Persistent queue and single worker.
- Fake extractor.
- Temporary token generation.
- Streaming download endpoint.
- Cleanup and startup recovery.

## Phase 3 — Real extractors

**Status: Planned**

- `yt-dlp` adapter.
- `ffprobe` metadata inspection.
- FFmpeg merge/remux behavior.
- `gallery-dl` adapter.
- Platform and error normalization.
- Optional real smoke-test command.

## Phase 4 — Packaging and documentation finalization

**Status: Planned**

- Finalize Dockerfile and Compose for the complete runtime.
- Complete the OpenAPI specification for every implemented endpoint.
- Validate and update the README local quick start for the completed system.
- Add the Shortcut construction guide.
- Finalize CI coverage for the complete feature set.
- Finalize security and self-hosting notes.

Phase 0 provides working baselines for several of these deliverables. Phase 4 completes them against the finished feature set; those baselines do not make Phase 4 active.

## Delivery rules

- Use Superpowers to create and approve the specification and implementation plan for each phase.
- Implement behavior with TDD and the coverage requirements in `AGENTS.md`.
- Update this document in the same change whenever a phase changes status or order.
- Do not create placeholder packages or speculative interfaces for later phases.
