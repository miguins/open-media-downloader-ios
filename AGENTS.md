# Project Rules

## Project

- Module: `github.com/miguins/open-media-downloader-ios`; executable: `omdi`.
- Phases 0–4.1 are complete; Phase 5 (bulk bundle downloads) is next. Current scope: `omdi serve`, `omdi keys create|list|revoke|purge`, `omdi jobs list|delete|purge`, `GET /healthz`, `GET /readyz`, authenticated `/v1/jobs` create/get/cancel, token-based `/v1/downloads/{token}`, a SQLite queue with one worker, real `yt-dlp` and `gallery-dl` extraction behind a validating egress proxy, a build-tagged fake extractor for the Bruno collection, periodic cleanup, trace-scoped JSON logs, fixed `error_detail` failure reasons, the production deployment in `compose.yaml`, and tag-triggered releases that publish a multi-architecture image to the GitHub Container Registry.
- Read `docs/roadmap.md` before planning work. It is the source of truth for implementation order, status, and the next milestone; update it whenever delivery status changes. `docs/architecture.md` describes current and target system boundaries but does not override the roadmap.
- Treat the Phase 1 and Phase 2 files under `docs/superpowers/records/` as historical delivery records, not implementation-plan templates. Starting with Phase 3, commit and obtain approval for the design specification and a current-format Superpowers implementation plan, then select the execution method before changing behavior.
- Docker Compose is the primary development environment. Use the `Makefile` targets instead of requiring host Go, Node.js, Bruno, or media tools.
- Keep `cmd/omdi` for composition and CLI parsing, `internal/config` for configuration, `internal/api` for HTTP contracts, `internal/app` for lifecycle, `internal/logging` for trace IDs and scoped log attributes, `internal/store` for SQLite and migrations, `internal/auth` for API keys, `internal/id` for identifiers, `internal/job` for domain models, `internal/urlpolicy` for URL and network destination policy, `internal/readiness` for dependency checks, `internal/storage` for the on-disk layout and extractor-output ingestion, `internal/worker` for job execution, `internal/extractor` for extractor implementations, and `internal/cleanup` for recovery and expiry. Do not create empty future packages or speculative interfaces.
- Keep the OpenAPI contract in `docs/openapi.yaml` and the executable Bruno collection in `collection/`. Container and media-tool definitions belong in `Dockerfile`, `compose.yaml` (production), `compose.development.yaml` (development, selected by the `Makefile`), and `docker/`. Use `make` targets for development; plain `docker compose` addresses production.

## Engineering

- Write all code, comments, documentation, API descriptions, and messages in English.
- Follow SOLID and idiomatic Go. Prefer the standard library, small packages, explicit dependencies, and simple concrete types; add interfaces only at real substitution boundaries.
- Use Superpowers for specification-driven development and TDD for every behavior change.
- Maintain at least 90% unit-test coverage. Security-critical code requires 100% coverage; `scripts/check-coverage.sh` enforces it for `internal/auth`, `internal/logging`, `internal/storage`, `internal/urlpolicy`, and `internal/extractor`, and new security-critical packages must be added there.
- Keep Go unit tests beside their packages; reserve root `tests/` for integration and end-to-end tests.
- Use only the newest stable dependency and tool releases. Never use alpha, beta, RC, nightly, preview, or floating versions.
- Use `make compose-up` (attached), `make compose-up-detached`, and `make compose-down` for the local service. `make collection-test`, `make real-smoke`, and therefore `make ci` stop the Compose service when they finish; restart it with `make compose-up-detached` if it was running. Use the other `Makefile` targets for formatting, tests, coverage, linting, security checks, and builds; run `make ci` before handoff when Docker is available.

## HTTP contracts

- Every new or changed HTTP endpoint must update its tests, OpenAPI definition, and organized Bruno request, assertions, and relevant environments in the same change.
- Name each Bruno request after the endpoint's final static path segment; for example, `/healthz` is named `healthz` and `POST /v1/jobs` is named `jobs`. When a path ends in a parameter or several methods share a path, use `<verb>-<resource>`, for example `get-job` and `cancel-job` for `/v1/jobs/{id}`.

## Security and privacy

- Treat submitted URLs and extractor output as attacker-controlled. Prevent SSRF, command injection, path traversal, symlink escapes, unsafe filenames, and unbounded resource use.
- Invoke subprocesses with argument arrays and context cancellation; never build shell commands from input.
- Never expose or log API keys, tokens, cookies, authorization headers, signed URLs, internal paths, or raw extractor diagnostics.
- Enforce authentication, ownership isolation, restrictive file permissions, process/file/time/disk limits, and verified dependency artifacts where applicable.
- Never commit secrets, personal data, real media, local paths, runtime data, or irrelevant editor, OS, and AI-tool artifacts.

## Git

- Keep commits conventional and atomic.
- Never commit or push without explicit user authorization.
