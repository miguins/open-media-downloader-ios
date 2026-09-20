# Project Rules

## Project

- Module: `github.com/miguins/open-media-downloader-ios`; executable: `omdi`.
- Phase 0 is complete; Phase 1 is next and has not started. Current scope is only `omdi serve` and unauthenticated `GET /healthz` returning `{"status":"ok"}\n`.
- Read `docs/roadmap.md` before planning work. It is the source of truth for implementation order, status, and the next milestone; update it whenever delivery status changes. `docs/architecture.md` describes the target system, not current behavior.
- Docker Compose is the primary development environment. Use the `Makefile` targets instead of requiring host Go, Node.js, Bruno, or media tools.
- Keep `cmd/omdi` for composition, `internal/config` for configuration, `internal/api` for HTTP contracts, and `internal/app` for lifecycle. Do not create empty future packages or speculative interfaces.
- Keep the OpenAPI contract in `docs/openapi.yaml` and the executable Bruno collection in `collection/`. Container and media-tool definitions belong in `Dockerfile`, `compose.yaml`, and `docker/`.

## Engineering

- Write all code, comments, documentation, API descriptions, and messages in English.
- Follow SOLID and idiomatic Go. Prefer the standard library, small packages, explicit dependencies, and simple concrete types; add interfaces only at real substitution boundaries.
- Use Superpowers for specification-driven development and TDD for every behavior change.
- Maintain at least 90% unit-test coverage. Security-critical code requires 100% coverage.
- Keep Go unit tests beside their packages; reserve root `tests/` for integration and end-to-end tests.
- Use only the newest stable dependency and tool releases. Never use alpha, beta, RC, nightly, preview, or floating versions.
- Use `make compose-up` and `make compose-down` for the local service. Use the other `Makefile` targets for formatting, tests, coverage, linting, security checks, and builds; run `make ci` before handoff when Docker is available.

## HTTP contracts

- Every new or changed HTTP endpoint must update its tests, OpenAPI definition, and organized Bruno request, assertions, and relevant environments in the same change.
- Name each Bruno request after the endpoint's final path segment; for example, `/healthz` is named `healthz`.

## Security and privacy

- Treat submitted URLs and extractor output as attacker-controlled. Prevent SSRF, command injection, path traversal, symlink escapes, unsafe filenames, and unbounded resource use.
- Invoke subprocesses with argument arrays and context cancellation; never build shell commands from input.
- Never expose or log API keys, tokens, cookies, authorization headers, signed URLs, internal paths, or raw extractor diagnostics.
- Enforce authentication, ownership isolation, restrictive file permissions, process/file/time/disk limits, and verified dependency artifacts where applicable.
- Never commit secrets, personal data, real media, local paths, runtime data, or irrelevant editor, OS, and AI-tool artifacts.

## Git

- Keep commits conventional and atomic.
- Never commit or push without explicit user authorization.
