# Initial Project Foundation Design

## Context

OpenMediaDownloaderIOS will become a self-hosted HTTP API that accepts public media URLs from an iOS Shortcut and produces temporary download links. This first delivery establishes a secure, reproducible project foundation without implementing media downloads, persistence, authentication, jobs, or extractors.

The repository name is `open-media-downloader-ios`, the Go module is `github.com/miguins/open-media-downloader-ios`, and the command-line executable is `omdi`.

## Goals

- Provide an idiomatic Go service with one liveness endpoint.
- Make Docker Compose the primary local development environment from day one.
- Establish testing, quality, security, and documentation gates suitable for a public repository.
- Provide a version-controlled Bruno collection that exercises the current HTTP contract.
- Include the complete external media-tool runtime so later features do not require replacing the development foundation.
- Preserve the seed's implementation order, with this completed foundation represented as Phase 0 before the functional phases, plus its essential product boundaries and security constraints.
- Keep the implementation small and avoid speculative packages or interfaces.

## Non-Goals

This delivery will not implement:

- SQLite or migrations.
- API keys, authentication, authorization, or rate limiting.
- Download jobs, workers, queues, media items, or download tokens.
- URL validation or extractor dispatch.
- Invocation of `yt-dlp`, `gallery-dl`, `ffmpeg`, or `ffprobe`.
- `/readyz` or any API other than `GET /healthz`.
- iOS Shortcut construction.
- Automated dependency-update pull requests.

## Runtime Behavior

The only application command is:

```text
omdi serve
```

It loads and validates minimal environment configuration, initializes structured logging, starts an HTTP server, and handles termination signals with graceful shutdown. Startup and runtime failures are logged without dumping the process environment and cause a non-zero exit.

The initial configuration is:

```text
OMDI_HTTP_ADDR=:8080
```

`GET /healthz` is unauthenticated and returns HTTP `200`, an `application/json` content type, and the exact response body `{"status":"ok"}` followed by one newline:

```json
{"status":"ok"}
```

The response must not contain build details, dependency versions, host information, configuration, or other infrastructure data.

## Go Architecture

The initial source boundaries are:

- `cmd/omdi`: minimal process entry point and dependency composition.
- `internal/config`: environment loading and validation for settings used by the current service.
- `internal/api`: Chi router construction and the health handler.
- `internal/app`: server lifecycle, signal-aware execution, structured logging, and graceful shutdown.

All behavior remains behind small concrete functions until a real substitution boundary justifies an interface. Future packages described by the product roadmap will not be created as empty directories, placeholder files, or unused abstractions.

The service uses the standard library wherever practical. Chi v5 is the only runtime Go dependency required for the initial endpoint. `log/slog` provides structured logging.

## Local Development and Packaging

Docker Compose is the supported default workflow. A host Go installation is optional.

The multi-stage `Dockerfile` provides:

- A development stage with the Go toolchain and the complete media-tool runtime.
- Build and test stages for reproducible compilation and verification.
- A production runtime stage containing `omdi`, `yt-dlp`, `gallery-dl`, `ffmpeg`, and `ffprobe`.
- A non-root runtime user and a writable `/data` directory owned by that user.

`compose.yaml` uses the development stage, publishes port `8080`, mounts only the Go module and current source directories read-only into the application service, supplies writable named volumes for Go caches, persists `/data`, and defines a health check against `/healthz`. It builds the current source into the writable cache before executing the resulting binary directly, allowing container signals to reach the application lifecycle. The listen-address default applies only when the variable is unset, so an explicitly empty value still fails validation. Project and Bruno `.env` files are not mounted into the application container.

The Compose configuration also defines an on-demand Bruno CLI service using the official image at an exact stable version and immutable digest. This service mounts only `collection/`, connects to the application through the Compose network, and does not expose the Bruno production `.env` file to the application container.

The `Makefile` wraps the Compose workflow and provides targets for bootstrap, tests, coverage, linting, vulnerability scanning, builds, Docker builds, Bruno collection tests, Compose startup, and Compose shutdown. Native equivalents are documented as an optional workflow.

## Bruno Collection

The repository contains an organized Bruno collection at `/collection` using the current OpenCollection YAML format:

```text
collection/
├── .env.example
├── .gitignore
├── opencollection.yml
├── environments/
│   ├── local.yml
│   └── production.yml
└── health/
    ├── folder.yml
    └── healthz.yml
```

The collection has exactly two environments:

- `local` sets `baseUrl` to `http://localhost:8080` for Bruno running on the host.
- `production` resolves `baseUrl` from `{{process.env.OMDI_BASE_URL}}`.

`collection/.env` is the collection-specific process environment file and is distinct from the project-root `.env`. It is ignored by Git and Docker build contexts. `collection/.env.example` contains only safe placeholders and documents `OMDI_BASE_URL`. Production environment files never contain concrete deployment URLs or secrets; future production-only values must also reference `process.env`.

`health/healthz.yml` is named `healthz`, calls `{{baseUrl}}/healthz`, and asserts HTTP status `200`, a JSON content type, and `res.body.status` equal to `ok`. Bruno request names always match the endpoint's final path segment. The request includes concise documentation and no private endpoint or response data; pre-request scripts are added only when request chaining requires them.

`make collection-test` runs the official Bruno CLI image through Compose. For container-to-container execution, it selects the `local` environment and overrides only `baseUrl` with the internal application service URL. This keeps the collection limited to the required `local` and `production` environments.

The structure and variable use follow the official Bruno documentation for [OpenCollection YAML](https://docs.usebruno.com/v3/opencollection-yaml/overview), [collection environments](https://docs.usebruno.com/v3/variables/environment-variables), [process environment variables](https://docs.usebruno.com/v3/variables/process-env), and the [official CLI container](https://docs.usebruno.com/v3/bru-cli/docker).

## Dependency Policy

At implementation time, every direct dependency, development tool, base image, GitHub Action, and external media tool is resolved to the latest production-stable release. Alpha, beta, release-candidate, nightly, preview, and floating `latest` references are forbidden.

When an upstream project publishes an LTS channel, the newest supported LTS release is used. When it does not, the newest general-availability stable release is used.

Selected versions are pinned and recorded. Container base images and GitHub Actions use immutable references where supported. Downloaded release artifacts are integrity-checked when upstream checksums are available. Go module checksums are committed in `go.sum`.

Updates are manual and documented. Dependabot or another automated update service is not configured in this delivery. CI runs vulnerability checks so a pinned dependency does not silently remain trusted after a disclosure.

## Security and Public Repository Hygiene

The initial implementation follows these controls:

- The health response discloses no operational details.
- Containers run as a non-root user.
- Example configuration contains placeholders and non-secret defaults only.
- `.gitignore` and `.dockerignore` exclude secrets, `.env` files, local databases, downloaded media, runtime data, logs, coverage output, build artifacts, editor files, operating-system files, and AI/tool artifacts.
- Documentation and examples contain no personal data, credentials, tokens, private URLs, machine-specific paths, or copyrighted media samples.
- The MIT copyright attribution uses the project contributors rather than a personal identity.
- CI checks known Go vulnerabilities, dependency integrity, tests, and container construction.

The repository instructions also preserve the security boundaries needed by later phases:

- Treat every submitted URL and every extractor result as attacker-controlled.
- Prevent SSRF with strict scheme and hostname allowlists plus public-IP validation and revalidation.
- Pass subprocess arguments directly with `exec.CommandContext`; never construct shell commands from input.
- Redact API keys, download tokens, cookies, authorization headers, and signed URLs from logs and errors.
- Enforce authentication, resource ownership, and indistinguishable cross-owner not-found responses.
- Prevent path traversal, symlink escapes, unsafe filenames, and permissive file access.
- Bound concurrency, execution time, file size, disk use, memory use, and captured subprocess output.
- Pin and verify dependencies, tool downloads, images, and CI actions.

## Repository Guidance

`AGENTS.md` remains concise and requires:

- A current project map covering the executable, implemented scope, package boundaries, API contracts, container files, and Compose-first workflow.
- English for all source code and documentation.
- SOLID principles and idiomatic Go without speculative abstraction.
- Superpowers for specification-driven development and TDD for every behavior change.
- At least 90% unit-test coverage and 100% coverage for security-critical logic.
- Go unit tests beside their packages, with root `tests/` reserved for integration and end-to-end suites.
- Current stable dependencies only, with no prereleases.
- Every new or changed HTTP endpoint must update its OpenAPI definition and its organized Bruno request, assertions, and relevant environments in the same change.
- Every Bruno request must be named after the endpoint's final path segment.
- Conventional, atomic commits.
- No commit or push without explicit user authorization.
- The project-specific security and public-repository controls summarized above.

`CLAUDE.md` contains only `@AGENTS.md`, keeping the repository guidance in one maintained source while making it available to future Claude sessions.

## Testing and CI

Implementation follows TDD: each behavior begins with a failing test, receives the smallest implementation that passes, and is then refactored while green.

Unit tests cover:

- Default configuration.
- Explicit valid configuration.
- Invalid configuration rejection.
- Health route status.
- Health response content type and exact decoded payload.
- Absence of diagnostic fields in the health response.
- Server lifecycle behavior that can be exercised deterministically without binding a public port.

The coverage gate measures the testable application packages under `internal/...` and requires at least 90%. Security-critical packages introduced later require 100%. The composition-only `cmd/omdi` entry point contains no business logic and is excluded from the percentage gate.

CI performs:

- Formatting verification.
- `go vet`.
- GolangCI-Lint.
- Unit tests with the race detector.
- Coverage threshold enforcement.
- `govulncheck`.
- Application compilation.
- Docker image construction.
- A container smoke test that waits for `/healthz` and verifies its response.
- The Bruno collection against the running Compose service, using the `local` environment with an internal `baseUrl` override.

Automated tests are deterministic and require no social-media, DNS, or general internet access after dependencies and images have been fetched.

## Documentation and Repository Files

The initial tracked repository includes:

- Go source and tests for the current runtime behavior.
- `README.md` with project purpose, current limitations, a Compose-first quick start, commands, architecture summary, dependency-update process, and links to roadmap status.
- `docs/openapi.yaml` describing only `/healthz`.
- `docs/architecture.md` preserving the future system boundaries and core security constraints.
- `docs/roadmap.md` preserving the ordered implementation phases, the completed Phase 0 foundation, current status, and explicit next milestone.
- `collection/` containing the OpenCollection YAML request and the `local` and `production` environments.
- `SECURITY.md` directing vulnerability reports to GitHub's private reporting mechanism without personal contact details.
- `AGENTS.md` containing the concise project map, workflow, and permanent repository rules.
- `CLAUDE.md` delegating to `AGENTS.md`.
- `LICENSE` containing the MIT license.
- Docker, Compose, Make, lint, coverage, environment-example, and CI configuration.

`PROJECT_SEED.md` is removed from the first deliverable and must not enter the repository history. Its durable roadmap, architecture, and security requirements remain in maintained project documentation. The empty local `.ai-jail` artifact is ignored and remains outside tracked project content.

## Acceptance Criteria

- `make compose-up` starts the service without requiring Go on the host.
- `GET http://localhost:8080/healthz` returns HTTP `200` and the exact body `{"status":"ok"}\n` without operational details.
- The development and production containers contain the selected stable media tools, although the application does not invoke them.
- Bruno can open the collection, select either `local` or `production`, and execute the health request with its assertions.
- `production` obtains its base URL from the ignored `collection/.env`; no production value is committed.
- `make collection-test` runs the collection against the Compose service without requiring Bruno or Node.js on the host.
- All Go code and documentation are in English.
- A zero-context agent can identify Phase 0 as complete and Phase 1 as the unstarted next development effort from `AGENTS.md` and `docs/roadmap.md`.
- Unit coverage is at least 90% for testable application packages.
- Formatting, vetting, linting, race-enabled tests, vulnerability checks, builds, Docker builds, and the container smoke test pass.
- No secret, personal data, local runtime data, downloaded media, or irrelevant tooling artifact is tracked.
- No empty future package or speculative interface is introduced.
- No commit or push occurs without explicit authorization.
