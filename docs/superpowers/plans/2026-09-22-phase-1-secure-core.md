# Phase 1 — Secure Core Implementation Plan

**Goal:** Deliver complete configuration, SQLite persistence, API-key management and authentication, job/item/token domain models, URL policy with SSRF defenses, and `GET /readyz`.

**Spec:** `docs/superpowers/specs/2026-09-22-phase-1-secure-core-design.md`

**Tech Stack additions:** `modernc.org/sqlite` v1.59.0, `golang.org/x/net/idna` (newest stable).

## Global Constraints

- TDD: write the failing test, watch it fail, implement, watch it pass.
- Run every Go command through Compose (`docker compose run --rm --no-deps tools ...` or `make` targets).
- `internal/auth` and `internal/urlpolicy` require 100% coverage; the `internal/...` total stays at least 90%.
- Errors and logs never contain secrets, submitted URLs, configured paths, or raw OS/tool output.
- No placeholder packages, routes, or interfaces beyond those named in the spec.
- No commit or push without explicit authorization.

## File Map

- `internal/config/config.go`, `config_test.go`: full configuration contract.
- `internal/store/store.go`: open, pragmas, permissions, migrations; `migrations/0001_initial.sql`.
- `internal/store/keys.go`, `jobs.go`, `errors.go` with tests: API-key, job, item, and download-token repositories.
- `internal/id/id.go`, `id_test.go`: random record identifiers.
- `internal/job/job.go`, `job_test.go`: statuses, transitions, error codes.
- `internal/auth/key.go`, `middleware.go` with tests: key format, hashing, verification, middleware.
- `internal/urlpolicy/normalize.go`, `address.go` with tests: normalization, allowlist, public-address classification, resolution and dial checks.
- `internal/readiness/readiness.go`, `readiness_test.go`: database, storage, and tool checks.
- `internal/api/router.go`, `readiness.go`, tests: `/readyz`.
- `cmd/omdi/main.go`, `keys.go`, tests: `serve` wiring and `keys` subcommands.
- `scripts/check-coverage.sh`: per-package 100% gate.
- `docs/openapi.yaml`, `collection/health/readyz.yml`: `/readyz` contract.
- `Makefile`, `compose.yaml`, `.env.example`, `README.md`, `AGENTS.md`, `docs/architecture.md`, `docs/roadmap.md`.

## Tasks

- [x] 1. Configuration contract: table-driven tests for every variable, default, bound, cross-field rule, aggregated errors, and non-echo of values; implement.
- [x] 2. Store: tests for directory/file modes, symlink refusal, pragmas, migration application, idempotence, and newer-schema refusal; implement with embedded SQL.
- [x] 3. Domain models: tests for ID format, every allowed and forbidden transition, and timestamps; implement.
- [x] 4. API keys: tests for generation, strict parsing, hashing, verification (valid, unknown, revoked, malformed, wrong secret); repository tests for create/get/list/revoke/touch; implement.
- [x] 5. Middleware: tests for missing, malformed, non-Bearer, query-string, revoked, valid, and store-failure cases, uniform `401`, context key ID, throttled `last_used_at`; implement.
- [x] 6. Job, item, and token repositories: owner-scoped queries, cross-owner not-found, transitions persisted, expired-token deletion; implement.
- [x] 7. URL policy: tests for every normalization rule, each platform, subdomains, look-alike domains, IDNA, IP literals, and every special-purpose range; resolver and `DialControl` tests with fakes; implement.
- [x] 8. Readiness and `/readyz`: check tests and handler tests for `200`/`503` bodies; OpenAPI and Bruno `readyz`; implement.
- [x] 9. CLI: `keys create|list|revoke` tests; `serve` opens the store and wires readiness; `make key-create`.
- [x] 10. Coverage gate, documentation, roadmap status, and `make ci`.
