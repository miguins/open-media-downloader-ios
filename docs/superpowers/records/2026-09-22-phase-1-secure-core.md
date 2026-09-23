# Phase 1 — Secure Core Delivery Record

> **Historical record:** This document was consolidated at the Phase 1 handoff after implementation. It records delivered scope and verification; it is not a pre-implementation plan in the current `superpowers:writing-plans` format and must not be used as a template for future phases.

**Delivered outcome:** Complete configuration, SQLite persistence, API-key management and authentication, job/item/token domain models, URL policy with SSRF defenses, and `GET /readyz`.

**Design:** `docs/superpowers/specs/2026-09-22-phase-1-secure-core-design.md`

**Tech Stack additions:** `modernc.org/sqlite` v1.59.0 and `golang.org/x/net/idna`.

## Delivery Constraints

- TDD: write the failing test, watch it fail, implement, watch it pass.
- Run every Go command through Compose (`docker compose run --rm --no-deps tools ...` or `make` targets).
- `internal/auth` and `internal/urlpolicy` require 100% coverage; the `internal/...` total stays at least 90%.
- Errors and logs never contain secrets, submitted URLs, configured paths, or raw OS/tool output.
- No placeholder packages, routes, or interfaces beyond those named in the design.
- No commit or push without explicit authorization.

## Delivered File Map

- `internal/config/config.go`, `config_test.go`: full configuration contract.
- `internal/store/store.go`: open, pragmas, permissions, migrations; `migrations/0001_initial.sql`.
- `internal/store/keys.go`, `jobs.go`, `errors.go` with tests: API-key, job, item, and download-token repositories.
- `internal/id/id.go`, `id_test.go`: random record identifiers.
- `internal/job/job.go`, `job_test.go`: statuses, transitions, and error codes.
- `internal/auth/key.go`, `middleware.go` with tests: key format, hashing, verification, and middleware.
- `internal/urlpolicy/normalize.go`, `address.go` with tests: normalization, allowlist, public-address classification, resolution, and dial checks.
- `internal/readiness/readiness.go`, `readiness_test.go`: database, storage, and tool checks.
- `internal/api/router.go`, `readiness.go`, tests: `/readyz`.
- `cmd/omdi/main.go`, `keys.go`, tests: `serve` wiring and `keys` subcommands.
- `scripts/check-coverage.sh`: per-package 100% gate.
- `docs/openapi.yaml`, `collection/health/readyz.yml`: `/readyz` contract.
- `Makefile`, `compose.yaml`, `.env.example`, `README.md`, `AGENTS.md`, `docs/architecture.md`, `docs/roadmap.md`.

## Delivered Work

- [x] Configuration contract, including defaults, bounds, cross-field rules, aggregated errors, and value redaction.
- [x] SQLite store, permissions, pragmas, embedded migrations, idempotence, and newer-schema refusal.
- [x] Identifier and job domain models with allowed and forbidden transition coverage.
- [x] API-key generation, parsing, hashing, verification, repositories, and bearer middleware.
- [x] Owner-scoped job, item, and token repositories.
- [x] URL normalization, platform allowlisting, IDNA handling, public-address classification, bounded resolution, and connect-time checks.
- [x] Dependency readiness checks and the `/readyz` HTTP, OpenAPI, and Bruno contracts.
- [x] `omdi keys create|list|revoke`, service composition, and Make targets.
- [x] Security-critical coverage gates, documentation, roadmap status, and the complete CI suite.
