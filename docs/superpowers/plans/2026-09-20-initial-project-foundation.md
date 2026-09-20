# Initial Project Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a secure, Compose-first Go project foundation whose only application behavior is `omdi serve` exposing `GET /healthz`, with a synchronized OpenAPI contract and Bruno collection.

**Architecture:** Keep `cmd/omdi` as composition-only code, put configuration validation in `internal/config`, HTTP routing in `internal/api`, and signal-aware server lifecycle in `internal/app`. Use a multi-stage Debian-family container with pinned media tools, Docker Compose for all routine development commands, and a Bruno OpenCollection YAML collection for executable API examples.

**Tech Stack:** Go 1.27.1, `net/http`, Chi v5.3.2, `log/slog`, Docker Compose, Python 3.14.7, Bruno CLI 4.1.0, GolangCI-Lint 2.13.2, govulncheck 1.8.0, yt-dlp 2026.08.19, gallery-dl 1.32.13, FFmpeg/ffprobe 9.0.2, OpenAPI 3.2.1, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-20-initial-project-foundation-design.md`

## Global Constraints

- Repository name: `open-media-downloader-ios`; module: `github.com/miguins/open-media-downloader-ios`; executable: `omdi`.
- The only application command is `omdi serve`; the only HTTP API is unauthenticated `GET /healthz` with exact body `{"status":"ok"}\n`.
- Docker Compose is the primary local workflow; Go, Node.js, Bruno, and media tools are optional on the host.
- All source code, comments, user-facing messages, and documentation are English.
- Apply SOLID and idiomatic Go without speculative interfaces or empty future packages.
- Follow TDD for behavior. Enforce at least 90% unit coverage under `internal/...`; future security-critical logic requires 100%.
- Use current stable releases only: no alpha, beta, RC, nightly, preview, or floating `latest` references.
- Pin Go 1.27.1, Chi 5.3.2, Python 3.14.7, Bruno CLI 4.1.0, GolangCI-Lint 2.13.2, govulncheck 1.8.0, yt-dlp 2026.08.19, gallery-dl 1.32.13, and FFmpeg 9.0.2.
- Do not configure Dependabot or another automated dependency-update service.
- The Bruno collection lives at `collection/` and has exactly two environments: `local` and `production`.
- Every endpoint change must update Go tests, `docs/openapi.yaml`, and the matching Bruno request/assertions in the same change.
- Name each Bruno request after the endpoint's final path segment; `/healthz` is named `healthz`.
- Do not track personal data, secrets, real media, machine-specific paths, runtime data, or irrelevant editor/AI artifacts.
- Remove `PROJECT_SEED.md` before the first commit; it must never enter Git history.
- Do not commit or push without explicit user authorization. If a commit is authorized, it must be conventional and atomic.

## Review Focus

- `OMDI_HTTP_ADDR` explicitly set to empty or whitespace must fail startup instead of silently using the default; Task 2 tests this.
- Malformed, missing-port, zero-port, and out-of-range listen addresses must fail without echoing the supplied value; Task 2 tests this.
- Unknown paths and non-GET health requests must return `404` and `405` respectively without leaking health data; Task 3 tests this.
- Cancellation before or during listener startup, cancellation with an in-flight request, and an occupied listen address must terminate cleanly and deterministically; Task 4 tests these cases. Compose must execute the built binary directly so `SIGTERM` reaches this lifecycle.
- Bruno must expose exactly the `local` and `production` environments and keep the `/healthz` request synchronized with the HTTP contract.

## File Map

### Application

- `cmd/omdi/main.go`: parse the single command, wire dependencies, install signal handling, and map startup errors to process exit codes.
- `internal/config/config.go`: load and validate `OMDI_HTTP_ADDR`.
- `internal/config/config_test.go`: table-driven configuration tests.
- `internal/api/router.go`: construct the Chi router.
- `internal/api/health.go`: implement the liveness response.
- `internal/api/router_test.go`: verify the complete public HTTP behavior.
- `internal/app/app.go`: listen, serve with defensive timeouts, and shut down gracefully.
- `internal/app/app_test.go`: deterministic listener, cancellation, in-flight request, and bind-failure tests.

### Development and packaging

- `go.mod`, `go.sum`: module, Chi, and pinned Go tools.
- `Dockerfile`: verified media-tool builders plus development, Go builder, and non-root runtime stages.
- `docker/media-tools.in`: direct Python media-tool requirements.
- `docker/media-tools.lock`: generated transitive requirements with hashes.
- `compose.yaml`: application, developer-tools, and Bruno CLI services.
- `Makefile`: Compose-backed developer and CI commands.
- `scripts/check-coverage.sh`: enforce the numeric coverage threshold.
- `.golangci.yml`: GolangCI-Lint v2 configuration.
- `.env.example`: safe project-local defaults.
- `.gitignore`, `.dockerignore`: public-repository and build-context hygiene.

### API contract and documentation

- `collection/opencollection.yml`: Bruno collection marker and metadata.
- `collection/environments/local.yml`: host-local base URL.
- `collection/environments/production.yml`: `process.env`-backed production URL.
- `collection/health/folder.yml`: health request group.
- `collection/health/healthz.yml`: request, assertions, and docs.
- `collection/.env.example`, `collection/.gitignore`: safe production template and secret exclusion.
- `docs/openapi.yaml`: OpenAPI 3.2.1 contract for `/healthz`.
- `docs/architecture.md`: durable product boundaries and future security model distilled from the seed.
- `docs/roadmap.md`: completed Phase 0 foundation, ordered functional phases, delivery status, and explicit next milestone distilled from the seed.
- `README.md`: Compose-first setup, commands, Bruno usage, current scope, and dependency-update procedure.
- `SECURITY.md`: private vulnerability reporting and supported-version policy.
- `AGENTS.md`: concise project map, workflow, and permanent development rules.
- `CLAUDE.md`: direct pointer to the shared repository instructions.
- `LICENSE`: MIT license attributed to project contributors.

### Automation

- `.github/workflows/ci.yml`: pinned, least-privilege CI entry point.

---

### Task 1: Establish repository guardrails and the pinned development environment

**Files:**
- Create: `AGENTS.md`
- Create: `CLAUDE.md`
- Create: `LICENSE`
- Create: `.gitignore`
- Create: `.dockerignore`
- Create: `.env.example`
- Create: `docker/media-tools.in`
- Create: `docker/media-tools.lock`
- Create: `Dockerfile`
- Create: `compose.yaml`
- Create: `go.mod`
- Create: `go.sum`
- Create: `.golangci.yml`

**Interfaces:**
- Consumes: Docker Engine and Docker Compose on the host.
- Produces: the `tools` Compose service used by every later Go task, the `development` and `runtime` image contracts, and the module/tool versions used by later tasks.

- [x] **Step 1: Add concise project orientation and permanent repository instructions**

Create `AGENTS.md` with the current scope, project structure, standard commands, HTTP-contract workflow, and enforceable engineering, security, privacy, and Git rules. Keep it compact enough to load in every future session, and make `CLAUDE.md` delegate to it with `@AGENTS.md` so both tools use one source of truth.

```markdown
# Project Rules

## Project

- Module: `github.com/miguins/open-media-downloader-ios`; executable: `omdi`.
- Current scope is only `omdi serve` and unauthenticated `GET /healthz` returning `{"status":"ok"}\n`. Treat everything in `docs/architecture.md` as future work.
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
- Use `make fmt`, `make test`, `make coverage`, `make lint`, `make vuln`, `make secret-scan`, and `make build` while developing. Run `make ci` before handoff when the environment supports Docker.

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
```

- [x] **Step 2: Add license, environment template, and exclusion rules**

Use the standard MIT text in `LICENSE` with `Copyright (c) 2026 OpenMediaDownloaderIOS contributors`.

Create `.env.example`:

```dotenv
OMDI_HTTP_ADDR=:8080
OMDI_DEV_UID=1000
OMDI_DEV_GID=1000
```

Create `.gitignore` with explicit coverage for secrets and project artifacts:

```gitignore
# Secrets and local configuration
.env
.env.*
!.env.example
collection/.env
collection/.env.*
!collection/.env.example

# Runtime data and downloaded media
data/
downloads/
*.sqlite
*.sqlite3
*.sqlite3-*
*.part
*.log

# Build and test output
bin/
dist/
coverage.out
coverage.html
reports/

# Editors and operating systems
.DS_Store
.idea/
.vscode/
*.swp

# Local agent and tool artifacts
.ai-jail
.claude/
.codex/
```

Create `.dockerignore` with the following exact rules. Keep the safe example files, source, tests, and documentation available to validation tooling:

```dockerignore
.git
.github
.env
.env.*
!.env.example
collection/.env
collection/.env.*
!collection/.env.example
data
downloads
*.sqlite
*.sqlite3
*.sqlite3-*
*.part
*.log
bin
dist
coverage.out
coverage.html
reports
.DS_Store
.idea
.vscode
*.swp
.ai-jail
.claude
.codex
.superpowers
PROJECT_SEED.md
```

- [x] **Step 3: Create and hash-lock the Python media-tool inputs**

Create `docker/media-tools.in`:

```text
gallery-dl==1.32.13
yt-dlp[default]==2026.8.19
```

Generate `docker/media-tools.lock` using pip-tools 7.6.1 inside the pinned Python image; do not hand-edit the generated file:

```bash
docker run --rm \
  --user "$(id -u):$(id -g)" \
  --env HOME=/tmp \
  --volume "$PWD:/workspace" \
  --workdir /workspace \
  python:3.14.7-slim-trixie@sha256:caaf356f40667c496d405780745b9ac25771c189a51dfcc42430d531ea09f8a2 \
  sh -ec 'python -m venv /tmp/pip-tools && /tmp/pip-tools/bin/pip install --no-cache-dir pip-tools==7.6.1 && /tmp/pip-tools/bin/pip-compile --index-url=https://pypi.org/simple --no-emit-index-url --no-emit-trusted-host --generate-hashes --strip-extras --resolver=backtracking --output-file=docker/media-tools.lock docker/media-tools.in'
```

Expected: every resolved package uses `==` and has one or more `--hash=sha256:` entries; no prerelease appears.

- [x] **Step 4: Create the verified multi-stage Dockerfile**

Use these immutable inputs:

```dockerfile
ARG GO_IMAGE=golang:1.27.1-trixie@sha256:433790e515d27dc6003e847e644cc0af956985cf315c1c58a3b73ee2dd305183
ARG DEBIAN_IMAGE=debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
ARG PYTHON_IMAGE=python:3.14.7-slim-trixie@sha256:caaf356f40667c496d405780745b9ac25771c189a51dfcc42430d531ea09f8a2
ARG FFMPEG_VERSION=9.0.2
ARG FFMPEG_SHA256=8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e
ARG FFMPEG_SIGNING_FINGERPRINT=FCF986EA15E6E293A5644F10B4322F04D67658D8
```

Implement these stages in `Dockerfile`:

1. `ffmpeg-builder`: install only build prerequisites; download `ffmpeg-9.0.2.tar.xz`, its `.asc`, and `https://ffmpeg.org/ffmpeg-devel.asc`; check SHA-256; verify the imported key fingerprint exactly; verify the signature; compile `ffmpeg` and `ffprobe` under `/opt/ffmpeg` with docs, debug data, and `ffplay` disabled.
2. `media-tools-builder`: create `/opt/media-tools` and install `docker/media-tools.lock` with `pip install --require-hashes`.
3. `development`: start from the pinned Python image, copy the Go toolchain from the pinned Go image and both media-tool trees, install only the compiler/Git prerequisites needed by Go development, create a non-root developer with configurable `DEV_UID`/`DEV_GID` build arguments (default `1000`), and set `PATH`, `GOMODCACHE`, and `GOCACHE`. Matching the host identity prevents Compose tooling from creating root-owned source files.
4. `builder`: copy the repository as the non-root user, run `go mod download`, and build `/tmp/omdi` with `CGO_ENABLED=0`, `-trimpath`, and stripped symbols.
5. `runtime`: start from the pinned Python image, copy the application and media tools, create the fixed non-root UID/GID `10001` and `/data`, expose `8080`, and set `ENTRYPOINT ["/usr/local/bin/omdi"]` plus `CMD ["serve"]`.

The runtime health check must use the already-required Python runtime to request `http://127.0.0.1:8080/healthz` and assert both status `200` and decoded JSON equality with `{"status":"ok"}`. Do not install curl solely for health checking.

The FFmpeg build must fail closed if either the digest or PGP verification fails. After compilation, run `ffmpeg -version`, `ffprobe -version`, and `ldd` in the builder so missing runtime libraries are detected during the image build.

Use this complete stage structure, retaining shell failure checks exactly:

```dockerfile
ARG GO_IMAGE=golang:1.27.1-trixie@sha256:433790e515d27dc6003e847e644cc0af956985cf315c1c58a3b73ee2dd305183
ARG DEBIAN_IMAGE=debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
ARG PYTHON_IMAGE=python:3.14.7-slim-trixie@sha256:caaf356f40667c496d405780745b9ac25771c189a51dfcc42430d531ea09f8a2
ARG FFMPEG_VERSION=9.0.2
ARG FFMPEG_SHA256=8c3850283eb25fa026482078a04051e0be17347b09ef81a0849bec15a96e002e
ARG FFMPEG_SIGNING_FINGERPRINT=FCF986EA15E6E293A5644F10B4322F04D67658D8

FROM ${DEBIAN_IMAGE} AS ffmpeg-builder
ARG FFMPEG_VERSION
ARG FFMPEG_SHA256
ARG FFMPEG_SIGNING_FINGERPRINT
RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        build-essential ca-certificates curl gnupg nasm pkg-config xz-utils yasm \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /tmp/ffmpeg
RUN set -eu; \
    curl --fail --show-error --silent --location --output ffmpeg.tar.xz \
        "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"; \
    curl --fail --show-error --silent --location --output ffmpeg.tar.xz.asc \
        "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz.asc"; \
    curl --fail --show-error --silent --location --output ffmpeg-devel.asc \
        "https://ffmpeg.org/ffmpeg-devel.asc"; \
    echo "${FFMPEG_SHA256}  ffmpeg.tar.xz" | sha256sum --check --strict; \
    fingerprint="$(gpg --batch --show-keys --with-colons ffmpeg-devel.asc | awk -F: '$1 == "fpr" { print $10; exit }')"; \
    test "$fingerprint" = "$FFMPEG_SIGNING_FINGERPRINT"; \
    gpg --batch --import ffmpeg-devel.asc; \
    gpg --batch --verify ffmpeg.tar.xz.asc ffmpeg.tar.xz; \
    tar --extract --xz --file ffmpeg.tar.xz --strip-components=1
RUN ./configure \
        --prefix=/opt/ffmpeg \
        --disable-autodetect \
        --disable-debug \
        --disable-doc \
        --disable-ffplay \
        --disable-shared \
        --enable-static \
        --enable-small \
    && make -j"$(nproc)" \
    && make install \
    && /opt/ffmpeg/bin/ffmpeg -version \
    && /opt/ffmpeg/bin/ffprobe -version \
    && (ldd /opt/ffmpeg/bin/ffmpeg > /tmp/ffmpeg-ldd.txt 2>&1 || true) \
    && (ldd /opt/ffmpeg/bin/ffprobe > /tmp/ffprobe-ldd.txt 2>&1 || true) \
    && ! grep -q "not found" /tmp/ffmpeg-ldd.txt \
    && ! grep -q "not found" /tmp/ffprobe-ldd.txt

FROM ${PYTHON_IMAGE} AS python-runtime

FROM ${GO_IMAGE} AS go-toolchain

FROM python-runtime AS media-tools-builder
COPY docker/media-tools.lock /tmp/media-tools.lock
RUN python -m venv /opt/media-tools \
    && /opt/media-tools/bin/pip install \
        --no-cache-dir \
        --require-hashes \
        --requirement /tmp/media-tools.lock \
    && /opt/media-tools/bin/yt-dlp --version \
    && /opt/media-tools/bin/gallery-dl --version

FROM python-runtime AS development
ARG DEV_UID=1000
ARG DEV_GID=1000
COPY --from=go-toolchain /usr/local/go/ /usr/local/go/
COPY --from=ffmpeg-builder /opt/ffmpeg/ /opt/ffmpeg/
COPY --from=media-tools-builder /opt/media-tools/ /opt/media-tools/
ENV PATH="/usr/local/go/bin:/opt/ffmpeg/bin:/opt/media-tools/bin:${PATH}" \
    GOMODCACHE=/go/pkg/mod \
    GOCACHE=/home/omdi/.cache/go-build \
    GOFLAGS=-buildvcs=false
RUN apt-get update \
    && apt-get install --yes --no-install-recommends build-essential ca-certificates git \
    && rm -rf /var/lib/apt/lists/* \
    && test "${DEV_UID}" -gt 0 \
    && test "${DEV_GID}" -gt 0 \
    && groupadd --gid "${DEV_GID}" omdi \
    && useradd --uid "${DEV_UID}" --gid "${DEV_GID}" --create-home --shell /bin/sh omdi \
    && mkdir -p /workspace /data /go/pkg/mod /home/omdi/.cache/go-build \
    && chown -R omdi:omdi /workspace /data /go /home/omdi
WORKDIR /workspace
USER omdi

FROM development AS builder
COPY --chown=omdi:omdi go.mod go.sum ./
RUN go mod download
COPY --chown=omdi:omdi . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/omdi ./cmd/omdi

FROM ${PYTHON_IMAGE} AS runtime
COPY --from=ffmpeg-builder /opt/ffmpeg/ /opt/ffmpeg/
COPY --from=media-tools-builder /opt/media-tools/ /opt/media-tools/
COPY --from=builder /tmp/omdi /usr/local/bin/omdi
ENV PATH="/opt/ffmpeg/bin:/opt/media-tools/bin:${PATH}"
RUN rm -rf \
        /usr/local/lib/python3.14/ensurepip \
        /usr/local/lib/python3.14/site-packages/pip \
        /usr/local/lib/python3.14/site-packages/pip-*.dist-info \
        /opt/media-tools/lib/python3.14/site-packages/pip \
        /opt/media-tools/lib/python3.14/site-packages/pip-*.dist-info \
    && rm -f \
        /usr/local/bin/pip \
        /usr/local/bin/pip3 \
        /usr/local/bin/pip3.14 \
        /opt/media-tools/bin/pip \
        /opt/media-tools/bin/pip3 \
        /opt/media-tools/bin/pip3.14 \
    && groupadd --gid 10001 omdi \
    && useradd --uid 10001 --gid 10001 --create-home --shell /bin/sh omdi \
    && mkdir -p /data \
    && chown omdi:omdi /data
USER omdi
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["python3", "-c", "import json, urllib.request; response = urllib.request.urlopen('http://127.0.0.1:8080/healthz', timeout=2); assert response.status == 200; assert json.load(response) == {'status': 'ok'}"]
ENTRYPOINT ["/usr/local/bin/omdi"]
CMD ["serve"]
```

- [x] **Step 5: Create the Compose-first development topology**

Create `compose.yaml` with project name `open-media-downloader-ios` and three services:

- `app`: build target `development`; build the current source into the writable Go cache and execute that binary directly so signals reach the application; bind only `cmd/`, `internal/`, `go.mod`, and `go.sum` read-only under `/workspace` so neither project nor Bruno `.env` files are visible; mount named Go module/build caches and `/data`; publish `127.0.0.1:8080:8080`; default `OMDI_HTTP_ADDR` only when it is unset; use a Python health check; enable `init`; drop all capabilities; set `no-new-privileges`; use a read-only root filesystem and `/tmp` tmpfs.
- `tools`: use the same development target under a `tools` profile; bind the repository read-write for formatting and dependency commands; reuse named Go caches; default to `go version`; do not publish ports.
- `bruno`: use `ghcr.io/usebruno/cli:4.1.0-debian@sha256:d127a19ba4cc4e195e54716bcc175cfb6d937f0cd48074525aaf9580149ca1d3`; mount `./collection:/bruno:ro`; depend on a healthy `app`; run the collection with `--env local --env-var baseUrl=http://app:8080`; drop capabilities and set `no-new-privileges`.

Define named volumes `go-mod-cache`, `go-build-cache`, and `app-data`. Do not mount the Docker socket into an application service.

Use this Compose structure:

```yaml
name: open-media-downloader-ios

x-development-build: &development-build
  context: .
  target: development
  args:
    DEV_UID: "${OMDI_DEV_UID:-1000}"
    DEV_GID: "${OMDI_DEV_GID:-1000}"

services:
  app:
    build: *development-build
    command: ["sh", "-ec", "mkdir -p \"$$GOTMPDIR\"; go build -trimpath -o /home/omdi/.cache/go-build/omdi-dev ./cmd/omdi; exec /home/omdi/.cache/go-build/omdi-dev serve"]
    init: true
    environment:
      GOTMPDIR: /home/omdi/.cache/go-build/tmp
      OMDI_HTTP_ADDR: "${OMDI_HTTP_ADDR-:8080}"
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./cmd:/workspace/cmd:ro
      - ./internal:/workspace/internal:ro
      - ./go.mod:/workspace/go.mod:ro
      - ./go.sum:/workspace/go.sum:ro
      - go-mod-cache:/go/pkg/mod
      - go-build-cache:/home/omdi/.cache/go-build
      - app-data:/data
    read_only: true
    tmpfs:
      - /tmp
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    stop_grace_period: 15s
    healthcheck:
      test:
        - CMD
        - python3
        - -c
        - >-
          import json, urllib.request;
          response = urllib.request.urlopen('http://127.0.0.1:8080/healthz', timeout=2);
          assert response.status == 200;
          assert json.load(response) == {'status': 'ok'}
      interval: 5s
      timeout: 3s
      start_period: 10s
      retries: 5

  tools:
    profiles: ["tools"]
    build: *development-build
    command: ["go", "version"]
    volumes:
      - .:/workspace
      - go-mod-cache:/go/pkg/mod
      - go-build-cache:/home/omdi/.cache/go-build
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true

  bruno:
    profiles: ["tools"]
    image: ghcr.io/usebruno/cli:4.1.0-debian@sha256:d127a19ba4cc4e195e54716bcc175cfb6d937f0cd48074525aaf9580149ca1d3
    command:
      - run
      - .
      - -r
      - --env
      - local
      - --env-var
      - baseUrl=http://app:8080
    volumes:
      - ./collection:/bruno:ro
    depends_on:
      app:
        condition: service_healthy
    read_only: true
    tmpfs:
      - /tmp
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true

volumes:
  go-mod-cache:
  go-build-cache:
  app-data:
```

- [x] **Step 6: Initialize the pinned Go module and tools through Compose**

Run:

```bash
docker compose --profile tools build tools
docker compose run --rm --no-deps tools go mod init github.com/miguins/open-media-downloader-ios
docker compose run --rm --no-deps tools go get github.com/go-chi/chi/v5@v5.3.2
docker compose run --rm --no-deps tools go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
docker compose run --rm --no-deps tools go get -tool golang.org/x/vuln/cmd/govulncheck@v1.8.0
docker compose run --rm --no-deps tools go mod edit -go=1.27.1 -toolchain=go1.27.1
docker compose run --rm --no-deps tools go mod download
```

Expected: `go.mod` contains the exact module and Go version, Chi version, and tool directives; `go.sum` contains checksums. A later `go mod tidy` may remove the redundant `toolchain` directive because it is identical to the patch-level `go` directive.

- [x] **Step 7: Add strict GolangCI-Lint v2 configuration**

Create `.golangci.yml` with `version: "2"`, a five-minute timeout, zero issue caps, the standard linter set, and explicit enables for `bodyclose`, `errorlint`, `gosec`, `misspell`, `nilerr`, `noctx`, `revive`, `staticcheck`, `unconvert`, `unused`, and `usestdlibvars`. Enable `gofmt` and `goimports` formatters. Do not suppress findings globally; use a narrowly documented exclusion only if generated code later requires one.

```yaml
version: "2"

run:
  timeout: 5m

linters:
  default: standard
  enable:
    - bodyclose
    - errorlint
    - gosec
    - misspell
    - nilerr
    - noctx
    - revive
    - staticcheck
    - unconvert
    - unused
    - usestdlibvars

formatters:
  enable:
    - gofmt
    - goimports

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
```

- [x] **Step 8: Verify the foundation and dependency versions**

Run:

```bash
docker compose config --quiet
docker compose run --rm --no-deps tools sh -ec '
  go version
  python --version
  yt-dlp --version
  gallery-dl --version
  ffmpeg -version | head -n 1
  ffprobe -version | head -n 1
'
grep -F 'github.com/go-chi/chi/v5 v5.3.2' go.mod
grep -F 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint' go.mod
grep -F 'golang.org/x/vuln/cmd/govulncheck' go.mod
git check-ignore -q .env
git check-ignore -q collection/.env
git check-ignore -q .ai-jail
```

Expected versions: Go 1.27.1, Python 3.14.7, Chi 5.3.2, GolangCI-Lint 2.13.2, govulncheck 1.8.0, yt-dlp 2026.08.19, gallery-dl 1.32.13, and FFmpeg/ffprobe 9.0.2. Execute the Go tools themselves after Task 3 adds the first source import and `go mod tidy` can complete without removing the intentionally predeclared application dependency.

- [x] **Step 9: Review checkpoint — do not commit**

Review only the repository policy, dependency pins, build-chain verification, ignored files, and Compose isolation. Preserve all changes uncommitted for the single authorized initial commit in Task 8.

---

### Task 2: Implement minimal configuration with TDD

**Files:**
- Create: `internal/config/config_test.go`
- Create: `internal/config/config.go`

**Interfaces:**
- Consumes: `OMDI_HTTP_ADDR` through an injected `func(string) (string, bool)` lookup.
- Produces: `type Config struct { HTTPAddr string }` and `func Load(lookup func(string) (string, bool)) (Config, error)` for `cmd/omdi`.

- [x] **Step 1: Write failing configuration tests**

Cover unset default, explicit IPv4/hostname/IPv6 host-port values, explicitly empty input, whitespace, missing port, non-numeric port, port zero, port above 65535, and error redaction. Use a table-driven test with a lookup closure and assert that invalid errors contain `OMDI_HTTP_ADDR` but never contain the supplied value.

Use this complete test:

```go
package config

import (
    "strings"
    "testing"
)

func TestLoad(t *testing.T) {
    tests := []struct {
        name      string
        value     string
        present   bool
        want      Config
        wantError bool
    }{
        {name: "unset uses default", want: Config{HTTPAddr: ":8080"}},
        {name: "IPv4 address", value: "127.0.0.1:9090", present: true, want: Config{HTTPAddr: "127.0.0.1:9090"}},
        {name: "hostname", value: "localhost:9090", present: true, want: Config{HTTPAddr: "localhost:9090"}},
        {name: "IPv6 address", value: "[::1]:9090", present: true, want: Config{HTTPAddr: "[::1]:9090"}},
        {name: "empty", value: "", present: true, wantError: true},
        {name: "whitespace", value: "  ", present: true, wantError: true},
        {name: "missing port", value: "localhost", present: true, wantError: true},
        {name: "non-numeric port", value: "localhost:secret-value", present: true, wantError: true},
        {name: "zero port", value: ":0", present: true, wantError: true},
        {name: "port too large", value: ":65536", present: true, wantError: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            lookup := func(string) (string, bool) { return tt.value, tt.present }
            got, err := Load(lookup)
            if tt.wantError {
                if err == nil || !strings.Contains(err.Error(), "OMDI_HTTP_ADDR") {
                    t.Fatalf("Load() error = %v", err)
                }
                if tt.value != "" && strings.Contains(err.Error(), tt.value) {
                    t.Fatalf("Load() leaked input in error: %v", err)
                }
                return
            }
            if err != nil || got != tt.want {
                t.Fatalf("Load() = %#v, %v; want %#v, nil", got, err, tt.want)
            }
        })
    }
}
```

- [x] **Step 2: Run the tests to verify RED**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/config
```

Expected: FAIL because `Config` and `Load` are undefined.

- [x] **Step 3: Implement the smallest validated configuration loader**

In `internal/config/config.go`, define `defaultHTTPAddr = ":8080"`, `Config`, and `Load`. Treat absence as default, but reject a present empty/whitespace value. Validate with `net.SplitHostPort`, `strconv.Atoi`, and the inclusive port range `1..65535`. Return safe errors such as `OMDI_HTTP_ADDR must use host:port syntax` and never interpolate the input value.

```go
package config

import (
    "errors"
    "net"
    "strconv"
    "strings"
)

const defaultHTTPAddr = ":8080"

type Config struct {
    HTTPAddr string
}

func Load(lookup func(string) (string, bool)) (Config, error) {
    value, present := lookup("OMDI_HTTP_ADDR")
    if !present {
        value = defaultHTTPAddr
    }
    if value == "" || value != strings.TrimSpace(value) {
        return Config{}, errors.New("OMDI_HTTP_ADDR must not be empty or contain surrounding whitespace")
    }

    _, port, err := net.SplitHostPort(value)
    if err != nil {
        return Config{}, errors.New("OMDI_HTTP_ADDR must use host:port syntax")
    }
    number, err := strconv.Atoi(port)
    if err != nil || number < 1 || number > 65535 {
        return Config{}, errors.New("OMDI_HTTP_ADDR must contain a port from 1 to 65535")
    }

    return Config{HTTPAddr: value}, nil
}
```

Add Go doc comments required by the configured linters without expanding the API.

- [x] **Step 4: Run tests and formatting to verify GREEN**

```bash
docker compose run --rm --no-deps tools gofmt -w internal/config
docker compose run --rm --no-deps tools go test ./internal/config
```

Expected: PASS.

- [x] **Step 5: Review checkpoint — do not commit**

Confirm the test actually proves error redaction and present-empty behavior. Preserve changes uncommitted.

---

### Task 3: Implement the health HTTP contract with TDD

**Files:**
- Create: `internal/api/router_test.go`
- Create: `internal/api/router.go`
- Create: `internal/api/health.go`

**Interfaces:**
- Consumes: no application dependency.
- Produces: `func NewRouter() http.Handler` for the application lifecycle.

- [x] **Step 1: Write failing router contract tests**

Use only `net/http/httptest` and the standard library. Assert status `200`, exact content type `application/json; charset=utf-8`, exact bytes `{"status":"ok"}\n`, decoded map length one, and no keys other than `status`. Add cases for `POST /healthz` returning `405` without the health body and `GET /readyz` returning `404` without the health body.

```go
package api

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestHealthz(t *testing.T) {
    request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
    response := httptest.NewRecorder()

    NewRouter().ServeHTTP(response, request)

    if response.Code != http.StatusOK {
        t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
    }
    if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
        t.Fatalf("Content-Type = %q", got)
    }
    if got, want := response.Body.String(), "{\"status\":\"ok\"}\n"; got != want {
        t.Fatalf("body = %q; want %q", got, want)
    }

    var body map[string]string
    if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
        t.Fatalf("decode response: %v", err)
    }
    if len(body) != 1 || body["status"] != "ok" {
        t.Fatalf("response disclosed unexpected fields: %#v", body)
    }
}

func TestRouterRejectsUnsupportedRequests(t *testing.T) {
    tests := []struct {
        name       string
        method     string
        path       string
        wantStatus int
    }{
        {name: "wrong method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
        {name: "unknown path", method: http.MethodGet, path: "/readyz", wantStatus: http.StatusNotFound},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            request := httptest.NewRequest(tt.method, tt.path, nil)
            response := httptest.NewRecorder()

            NewRouter().ServeHTTP(response, request)

            if response.Code != tt.wantStatus {
                t.Fatalf("status = %d; want %d", response.Code, tt.wantStatus)
            }
            if response.Body.String() == "{\"status\":\"ok\"}\n" {
                t.Fatal("unsupported request returned the health response")
            }
        })
    }
}
```

- [x] **Step 2: Run the tests to verify RED**

```bash
docker compose run --rm --no-deps tools go test ./internal/api
```

Expected: FAIL because `NewRouter` is undefined.

- [x] **Step 3: Implement the minimal Chi router and static response**

`internal/api/router.go`:

```go
package api

import (
    "net/http"

    "github.com/go-chi/chi/v5"
)

func NewRouter() http.Handler {
    router := chi.NewRouter()
    router.Get("/healthz", healthz)
    return router
}
```

`internal/api/health.go`:

```go
package api

import (
    "io"
    "net/http"
)

const healthBody = "{\"status\":\"ok\"}\n"

func healthz(response http.ResponseWriter, _ *http.Request) {
    response.Header().Set("Content-Type", "application/json; charset=utf-8")
    response.WriteHeader(http.StatusOK)
    if _, err := io.WriteString(response, healthBody); err != nil {
        return
    }
}
```

Add a doc comment to `NewRouter`. Do not add recovery, request logging, CORS, version headers, or unrelated middleware.

- [x] **Step 4: Run tests, tidy modules, and verify GREEN**

```bash
docker compose run --rm --no-deps tools gofmt -w internal/api
docker compose run --rm --no-deps tools go mod tidy
docker compose run --rm --no-deps tools go test ./internal/api
```

Expected: PASS; Chi remains exactly v5.3.2.

- [x] **Step 5: Review checkpoint — do not commit**

Confirm that only one route is registered and the response contains no diagnostics. Preserve changes uncommitted.

---

### Task 4: Implement graceful application lifecycle and CLI wiring with TDD

**Files:**
- Create: `internal/app/app_test.go`
- Create: `internal/app/app.go`
- Create: `cmd/omdi/main.go`

**Interfaces:**
- Consumes: `config.Config.HTTPAddr`, `api.NewRouter()`, `context.Context`, `http.Handler`, and `*slog.Logger`.
- Produces: `func Run(ctx context.Context, address string, handler http.Handler, logger *slog.Logger) error` and the `omdi serve` process.

- [x] **Step 1: Write failing lifecycle tests**

In same-package tests, exercise an unexported `serve(ctx, listener, handler, logger)` so the test controls a `127.0.0.1:0` listener. Use `slog.New(slog.NewJSONHandler(io.Discard, nil))`.

Tests must prove:

1. A request reaches a simple handler, then cancellation returns `nil` within one second.
2. A context canceled before `serve` returns cleanly.
3. An in-flight handler blocks shutdown until the handler is released, then shutdown succeeds.
4. `Run` against an already occupied address returns an error containing `listen`.
5. `Run` does not begin listener startup when its context is already canceled.
6. Cancellation propagates to a listener startup that is still pending.

Use channels rather than sleeps to coordinate the in-flight test. Permit a bounded retry loop only when waiting for the server to begin accepting connections.

Use this complete test file:

```go
package app

import (
    "context"
    "errors"
    "io"
    "log/slog"
    "net"
    "net/http"
    "strings"
    "testing"
    "time"
)

func TestServeHandlesRequestAndStopsOnCancellation(t *testing.T) {
    listener := newTestListener(t)
    ctx, cancel := context.WithCancel(context.Background())
    result := make(chan error, 1)
    go func() {
        result <- serve(ctx, listener, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
            response.WriteHeader(http.StatusNoContent)
        }), testLogger())
    }()

    response := getEventually(t, "http://"+listener.Addr().String())
    if response.StatusCode != http.StatusNoContent {
        t.Fatalf("status = %d; want %d", response.StatusCode, http.StatusNoContent)
    }
    if err := response.Body.Close(); err != nil {
        t.Fatalf("close response body: %v", err)
    }

    cancel()
    requireResult(t, result, nil)
}

func TestServeWithCanceledContext(t *testing.T) {
    listener := newTestListener(t)
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    if err := serve(ctx, listener, http.NotFoundHandler(), testLogger()); err != nil {
        t.Fatalf("serve() error = %v; want nil", err)
    }
}

func TestServeWaitsForInFlightRequest(t *testing.T) {
    listener := newTestListener(t)
    started := make(chan struct{})
    release := make(chan struct{})
    defer func() {
        select {
        case <-release:
        default:
            close(release)
        }
    }()
    handler := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
        close(started)
        <-release
        response.WriteHeader(http.StatusNoContent)
    })
    ctx, cancel := context.WithCancel(context.Background())
    serveResult := make(chan error, 1)
    go func() {
        serveResult <- serve(ctx, listener, handler, testLogger())
    }()

    type requestResult struct {
        response *http.Response
        err      error
    }
    requestDone := make(chan requestResult, 1)
    request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String(), nil)
    if err != nil {
        t.Fatalf("create request: %v", err)
    }
    go func() {
        response, requestErr := http.DefaultClient.Do(request)
        requestDone <- requestResult{response: response, err: requestErr}
    }()

    select {
    case <-started:
    case <-time.After(time.Second):
        t.Fatal("handler did not start")
    }
    cancel()
    select {
    case err := <-serveResult:
        t.Fatalf("serve returned before in-flight request completed: %v", err)
    case <-time.After(100 * time.Millisecond):
    }

    close(release)
    select {
    case result := <-requestDone:
        if result.err != nil {
            t.Fatalf("request failed: %v", result.err)
        }
        if err := result.response.Body.Close(); err != nil {
            t.Fatalf("close response body: %v", err)
        }
    case <-time.After(time.Second):
        t.Fatal("request did not finish")
    }
    requireResult(t, serveResult, nil)
}

func TestRunRejectsOccupiedAddressWithoutLeakingIt(t *testing.T) {
    listener := newTestListener(t)

    err := Run(context.Background(), listener.Addr().String(), http.NotFoundHandler(), testLogger())
    if err == nil || !strings.Contains(err.Error(), "listen") {
        t.Fatalf("Run() error = %v; want sanitized listen error", err)
    }
    if strings.Contains(err.Error(), listener.Addr().String()) {
        t.Fatalf("Run() leaked configured address: %v", err)
    }
}

func TestNormalizeServeError(t *testing.T) {
    if err := normalizeServeError(http.ErrServerClosed); err != nil {
        t.Fatalf("normalizeServeError(http.ErrServerClosed) = %v", err)
    }
    sentinel := errors.New("sentinel")
    if err := normalizeServeError(sentinel); !errors.Is(err, sentinel) {
        t.Fatalf("normalizeServeError() = %v; want wrapped sentinel", err)
    }
}

func newTestListener(t *testing.T) net.Listener {
    t.Helper()
    listener, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatalf("listen: %v", err)
    }
    t.Cleanup(func() { _ = listener.Close() })
    return listener
}

func testLogger() *slog.Logger {
    return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func getEventually(t *testing.T, url string) *http.Response {
    t.Helper()
    deadline := time.Now().Add(time.Second)
    for {
        requestCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
        request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
        if err != nil {
            cancel()
            t.Fatalf("create request: %v", err)
        }
        response, err := http.DefaultClient.Do(request)
        cancel()
        if err == nil {
            return response
        }
        if time.Now().After(deadline) {
            t.Fatalf("server did not accept requests: %v", err)
        }
        time.Sleep(10 * time.Millisecond)
    }
}

func requireResult(t *testing.T, result <-chan error, want error) {
    t.Helper()
    select {
    case err := <-result:
        if !errors.Is(err, want) {
            t.Fatalf("serve() error = %v; want %v", err, want)
        }
    case <-time.After(time.Second):
        t.Fatal("serve did not stop")
    }
}
```

- [x] **Step 2: Run lifecycle tests to verify RED**

```bash
docker compose run --rm --no-deps tools go test ./internal/app
```

Expected: FAIL because `Run` and `serve` are undefined.

- [x] **Step 3: Implement defensive HTTP server lifecycle**

Create `internal/app/app.go` with these constants:

```go
const (
    readHeaderTimeout = 5 * time.Second
    readTimeout       = 15 * time.Second
    writeTimeout      = 15 * time.Second
    idleTimeout       = 60 * time.Second
    shutdownTimeout   = 10 * time.Second
)
```

Use this complete implementation. The returned bind error is deliberately sanitized so a configured host value is never copied into logs:

```go
package app

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "net"
    "net/http"
    "time"
)

const (
    readHeaderTimeout = 5 * time.Second
    readTimeout       = 15 * time.Second
    writeTimeout      = 15 * time.Second
    idleTimeout       = 60 * time.Second
    shutdownTimeout   = 10 * time.Second
)

// Run starts the HTTP server and blocks until it stops.
func Run(ctx context.Context, address string, handler http.Handler, logger *slog.Logger) error {
    listenConfig := &net.ListenConfig{}
    return runWithListener(ctx, address, handler, logger, listenConfig.Listen)
}

func runWithListener(
    ctx context.Context,
    address string,
    handler http.Handler,
    logger *slog.Logger,
    listen func(context.Context, string, string) (net.Listener, error),
) error {
    select {
    case <-ctx.Done():
        return nil
    default:
    }

    listener, err := listen(ctx, "tcp", address)
    if err != nil {
        return normalizeListenError(ctx)
    }

    return serve(ctx, listener, handler, logger)
}

func normalizeListenError(ctx context.Context) error {
    select {
    case <-ctx.Done():
        return nil
    default:
        return errors.New("listen: unable to bind configured address")
    }
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler, logger *slog.Logger) error {
    server := &http.Server{
        Handler:           handler,
        ReadHeaderTimeout: readHeaderTimeout,
        ReadTimeout:       readTimeout,
        WriteTimeout:      writeTimeout,
        IdleTimeout:       idleTimeout,
    }
    serveError := make(chan error, 1)
    go func() {
        serveError <- server.Serve(listener)
    }()

    select {
    case err := <-serveError:
        return normalizeServeError(err)
    case <-ctx.Done():
    }

    logger.Info("shutting down HTTP server")
    shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
    defer cancel()
    if err := server.Shutdown(shutdownContext); err != nil {
        if closeErr := server.Close(); closeErr != nil {
            return errors.Join(
                fmt.Errorf("shut down HTTP server: %w", err),
                fmt.Errorf("close HTTP server: %w", closeErr),
            )
        }
        return fmt.Errorf("shut down HTTP server: %w", err)
    }

    return normalizeServeError(<-serveError)
}

func normalizeServeError(err error) error {
    if err == nil || errors.Is(err, http.ErrServerClosed) {
        return nil
    }
    return fmt.Errorf("serve HTTP: %w", err)
}
```

- [x] **Step 4: Run lifecycle tests to verify GREEN**

```bash
docker compose run --rm --no-deps tools gofmt -w internal/app
docker compose run --rm --no-deps tools go test ./internal/app
```

Expected: PASS under repeated execution:

```bash
docker compose run --rm --no-deps tools go test -count=20 ./internal/app
```

- [x] **Step 5: Wire the composition-only command**

Create `cmd/omdi/main.go` with composition only:

```go
package main

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "github.com/miguins/open-media-downloader-ios/internal/api"
    "github.com/miguins/open-media-downloader-ios/internal/app"
    "github.com/miguins/open-media-downloader-ios/internal/config"
)

func main() {
    os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
    logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
    if len(args) != 1 || args[0] != "serve" {
        logger.Error("usage: omdi serve")
        return 2
    }

    cfg, err := config.Load(os.LookupEnv)
    if err != nil {
        logger.Error("invalid configuration", "error", err)
        return 1
    }

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    if err := app.Run(ctx, cfg.HTTPAddr, api.NewRouter(), logger); err != nil {
        logger.Error("application stopped unexpectedly", "error", err)
        return 1
    }
    return 0
}
```

This implementation:

1. Builds a JSON `slog` logger on stderr.
2. Accepts exactly `omdi serve`; any other argument count or command logs `usage: omdi serve` and exits `2`.
3. Calls `config.Load(os.LookupEnv)` and exits `1` with a sanitized `invalid configuration` log on failure.
4. Creates a signal context for `os.Interrupt` and `syscall.SIGTERM`.
5. Calls `app.Run(ctx, cfg.HTTPAddr, api.NewRouter(), logger)` and exits `1` on an unexpected error.
6. Contains no business logic, global mutable state, build metadata, or environment dump.

- [x] **Step 6: Verify compilation, cancellation, and race behavior**

```bash
docker compose run --rm --no-deps tools go test -race ./...
docker compose run --rm --no-deps tools sh -ec '
  go build -trimpath -o /tmp/omdi ./cmd/omdi
  set +e
  /tmp/omdi invalid >/tmp/stdout 2>/tmp/stderr
  status=$?
  set -e
  test "$status" -eq 2
  ! grep -q "OMDI_" /tmp/stderr
'
```

Expected: tests and build pass; the invalid command exits `2` and prints no environment values.

- [x] **Step 7: Review checkpoint — do not commit**

Confirm shutdown does not leak a goroutine, listen errors are actionable but sanitized, and main remains composition-only. Preserve changes uncommitted.

---

### Task 5: Add repeatable quality, coverage, build, and security commands

**Files:**
- Create: `scripts/check-coverage.sh`
- Create: `Makefile`
- Modify: `compose.yaml`
- Modify: `Dockerfile`

**Interfaces:**
- Consumes: the `tools`, `app`, and `bruno` Compose services and all Go packages.
- Produces: stable developer commands and the `omdi:local` runtime image used by CI scanning.

- [x] **Step 1: Create the coverage threshold script**

Create `scripts/check-coverage.sh` using POSIX shell and mark it executable:

```sh
#!/bin/sh
set -eu

profile=${1:?usage: check-coverage.sh COVERAGE_PROFILE}
minimum=${COVERAGE_MIN:-90}
coverage=$(go tool cover -func="$profile" | awk '$1 == "total:" { gsub(/%/, "", $3); print $3 }')

if [ -z "$coverage" ]; then
    echo "unable to read total unit coverage" >&2
    exit 1
fi

if ! awk -v coverage="$coverage" -v minimum="$minimum" 'BEGIN { exit !(coverage + 0 >= minimum + 0) }'; then
    echo "unit coverage: ${coverage}% (minimum: ${minimum}%)" >&2
    exit 1
fi

echo "unit coverage: ${coverage}% (minimum: ${minimum}%)"
```

It accepts a coverage profile path as argument one and reads `COVERAGE_MIN` with default `90`. It fails if the total is missing or below threshold and compares the unrounded decimal value.

Mark it executable.

- [x] **Step 2: Prove the coverage script fails below the threshold**

Run current tests with a deliberately impossible threshold:

```bash
docker compose run --rm --no-deps tools sh -ec '
  go test -covermode=atomic -coverpkg=./internal/... -coverprofile=/tmp/coverage.out ./internal/...
  ! COVERAGE_MIN=101 ./scripts/check-coverage.sh /tmp/coverage.out
'
```

Expected: PASS because the inner coverage check fails as intended.

- [x] **Step 3: Create the Compose-backed Makefile**

Create this Makefile. Recipe indentation must be literal tabs:

```make
COMPOSE := docker compose
TOOLS_RUN := $(COMPOSE) run --rm --no-deps tools
TRIVY_IMAGE := aquasec/trivy:0.74.0@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969

.PHONY: bootstrap fmt fmt-check test coverage lint vuln secret-scan build docker-build image-scan smoke compose-up compose-down collection-test ci

bootstrap:
	command -v docker >/dev/null
	$(COMPOSE) version
	$(COMPOSE) build tools
	$(TOOLS_RUN) go mod download
	$(TOOLS_RUN) sh -ec 'go version; python --version; go tool golangci-lint version; go tool govulncheck -version; yt-dlp --version; gallery-dl --version; ffmpeg -version | head -n 1; ffprobe -version | head -n 1'

fmt:
	$(TOOLS_RUN) sh -ec 'gofmt -w $$(find cmd internal -type f -name "*.go")'

fmt-check:
	$(TOOLS_RUN) sh -ec 'files=$$(gofmt -l cmd internal); test -z "$$files" || { printf "%s\n" "$$files"; exit 1; }'

test:
	$(TOOLS_RUN) go test -race -count=1 ./...

coverage:
	$(TOOLS_RUN) sh -ec 'go test -covermode=atomic -coverpkg=./internal/... -coverprofile=/tmp/coverage.out ./internal/... && ./scripts/check-coverage.sh /tmp/coverage.out'

lint:
	$(TOOLS_RUN) sh -ec 'go vet ./... && go tool golangci-lint run'

vuln:
	$(TOOLS_RUN) go tool govulncheck ./...

secret-scan:
	docker run --rm --volume "$(CURDIR):/workspace:ro" $(TRIVY_IMAGE) fs --scanners secret --exit-code 1 --skip-dirs /workspace/.git /workspace

build:
	$(TOOLS_RUN) go build -trimpath -o /tmp/omdi ./cmd/omdi

docker-build:
	docker build --target runtime --tag omdi:local .

image-scan: docker-build
	docker run --rm --volume /var/run/docker.sock:/var/run/docker.sock:ro $(TRIVY_IMAGE) image --scanners vuln --exit-code 1 --ignore-unfixed --severity HIGH,CRITICAL omdi:local

smoke: docker-build
	@set -eu; container_id=$$(docker run --rm --detach omdi:local); trap 'docker stop "$$container_id" >/dev/null 2>&1 || true' EXIT INT TERM; attempts=0; until docker exec "$$container_id" python3 -c 'import json, urllib.request; response = urllib.request.urlopen("http://127.0.0.1:8080/healthz", timeout=2); assert response.status == 200; assert json.load(response) == {"status": "ok"}' >/dev/null 2>&1; do attempts=$$((attempts + 1)); test "$$attempts" -lt 30; sleep 1; done

compose-up:
	$(COMPOSE) up --build

compose-down:
	$(COMPOSE) down --remove-orphans

collection-test:
	@set -eu; trap '$(COMPOSE) down --remove-orphans' EXIT INT TERM; $(COMPOSE) up --build --detach --wait app; $(COMPOSE) run --rm bruno

ci:
	$(MAKE) fmt-check
	$(MAKE) test
	$(MAKE) coverage
	$(MAKE) lint
	$(MAKE) vuln
	$(MAKE) secret-scan
	$(MAKE) build
	$(MAKE) docker-build smoke image-scan
	$(MAKE) collection-test
```

The targets provide these contracts:

- `bootstrap`: verify Docker and Compose, build the `tools` image, download Go modules, and print pinned tool versions.
- `fmt`: run `gofmt -w` through the `tools` service.
- `fmt-check`: list unformatted Go files and fail when the list is non-empty.
- `test`: `go test -race -count=1 ./...`.
- `coverage`: generate `/tmp/coverage.out` for `./internal/...` with `-coverpkg=./internal/...` and call the threshold script.
- `lint`: `go vet ./...` followed by `go tool golangci-lint run`.
- `vuln`: `go tool govulncheck ./...`.
- `secret-scan`: scan repository files with the pinned Trivy image and fail on detected secrets.
- `build`: compile `/tmp/omdi` with `-trimpath`.
- `docker-build`: build target `runtime` as `omdi:local`.
- `image-scan`: run `aquasec/trivy:0.74.0@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969` against `omdi:local`, scanning vulnerabilities and failing on fixed HIGH or CRITICAL findings without suppressing a vulnerability ID.
- `smoke`: start the production image without publishing a port, poll its internal health endpoint, validate the exact JSON payload, and always stop it.
- `compose-up`: `docker compose up --build`.
- `compose-down`: remove Compose containers and orphans but preserve named data unless the caller explicitly requests volume removal.
- `collection-test`: start `app` with `--wait`, then run the Bruno service.
- `ci`: run format check, tests, coverage, lint, vulnerability scan, build, runtime image build, production smoke, image scan, and collection test in that order.

Every command must run through Compose or a digest-pinned container; host-native equivalents are documentation conveniences, not prerequisites.

- [x] **Step 4: Verify all local quality commands**

```bash
make fmt
make fmt-check
make test
make coverage
make lint
make vuln
make build
```

Expected: every target passes, and reported unit coverage is at least 90%.

- [x] **Step 5: Build and inspect the production image**

```bash
make docker-build
docker run --rm --entrypoint sh omdi:local -ec '
  test "$(id -u)" != "0"
  test -w /data
  yt-dlp --version
  gallery-dl --version
  ffmpeg -version | head -n 1
  ffprobe -version | head -n 1
'
```

Expected: non-root UID; writable `/data`; exact pinned tool versions.

- [x] **Step 6: Review checkpoint — do not commit**

Confirm commands work without host Go/Node/Bruno, failures propagate non-zero, and no scanner ignore file was introduced. Preserve changes uncommitted.

---

### Task 6: Add the organized Bruno collection and executable assertions

**Files:**
- Create: `collection/opencollection.yml`
- Create: `collection/.gitignore`
- Create: `collection/.env.example`
- Create: `collection/environments/local.yml`
- Create: `collection/environments/production.yml`
- Create: `collection/health/folder.yml`
- Create: `collection/health/healthz.yml`

**Interfaces:**
- Consumes: `GET {{baseUrl}}/healthz`; Bruno process variable `OMDI_BASE_URL` for production.
- Produces: an executable, organized API example selected by `--env local` or `--env production`.

- [x] **Step 1: Create the collection root and secret hygiene files**

`collection/opencollection.yml`:

```yaml
opencollection: 1.0.0

info:
  name: OpenMediaDownloaderIOS API
bundled: false
extensions:
  bruno:
    ignore:
      - node_modules
      - .git
```

`collection/.gitignore`:

```gitignore
.env
.env.*
!.env.example
reports/
```

`collection/.env.example`:

```dotenv
OMDI_BASE_URL=https://api.example.invalid
```

- [x] **Step 2: Create exactly two environments**

`collection/environments/local.yml`:

```yaml
name: local
variables:
  - name: baseUrl
    value: http://localhost:8080
```

`collection/environments/production.yml`:

```yaml
name: production
variables:
  - name: baseUrl
    value: "{{process.env.OMDI_BASE_URL}}"
```

Do not add a CI, staging, development, or Docker environment.

- [x] **Step 3: Create the organized health folder and request**

`collection/health/folder.yml`:

```yaml
info:
  name: health
  type: folder
```

`collection/health/healthz.yml` must contain:

```yaml
info:
  name: healthz
  type: http
  seq: 1
  tags:
    - smoke

http:
  method: GET
  url: "{{baseUrl}}/healthz"
  auth: inherit

runtime:
  scripts:
    - type: tests
      code: |-
        test("returns only the liveness status", function () {
          expect(res.getBody()).to.deep.equal({ status: "ok" });
        });
  assertions:
    - expression: res.status
      operator: eq
      value: "200"
    - expression: res.headers['content-type']
      operator: contains
      value: application/json
    - expression: res.body.status
      operator: eq
      value: ok

settings:
  encodeUrl: true
  timeout: 5000
  followRedirects: false
  maxRedirects: 0
  forwardAuthorizationHeader: true

docs: |-
  # healthz

  Returns the minimal unauthenticated liveness status. It does not expose dependency, host, build, or configuration details.
```

- [x] **Step 4: Verify collection structure and naming**

Run:

```bash
test "$(find collection/environments -maxdepth 1 -type f -name '*.yml' | wc -l)" -eq 2
grep -F 'name: healthz' collection/health/healthz.yml
grep -F 'url: "{{baseUrl}}/healthz"' collection/health/healthz.yml
! grep -F 'type: before-request' collection/health/healthz.yml
git check-ignore -q collection/.env
```

Expected: exactly two environments, the request name matches the final path segment, no unnecessary pre-request script exists, and the collection-local `.env` is ignored.

- [x] **Step 5: Verify the local collection against the running app**

```bash
docker compose up --build --detach --wait app
docker compose run --rm bruno
```

Expected: one request and all assertions pass. Verify `docker compose logs app` contains no authorization data or environment dump.

- [x] **Step 6: Review checkpoint — do not commit**

Confirm there are exactly two environment files, `collection/.env` is ignored, `.env.example` is tracked, production contains no concrete URL, and the request is under `health/`. Preserve changes uncommitted.

---

### Task 7: Add public documentation and retire the seed

**Files:**
- Create: `docs/openapi.yaml`
- Create: `docs/architecture.md`
- Create: `docs/roadmap.md`
- Create: `README.md`
- Create: `SECURITY.md`
- Delete: `PROJECT_SEED.md`

**Interfaces:**
- Consumes: the implemented command, endpoint, Compose/Make commands, dependency pins, and future product constraints from the seed.
- Produces: public setup, API contract, security-reporting, architecture, and ordered roadmap documentation with no personal data.

- [x] **Step 1: Write the OpenAPI 3.2.1 contract**

Create `docs/openapi.yaml` with this complete contract:

```yaml
openapi: 3.2.1
info:
  title: OpenMediaDownloaderIOS API
  version: 0.1.0
servers:
  - url: http://localhost:8080
paths:
  /healthz:
    get:
      operationId: getHealth
      summary: Check service liveness
      security: []
      responses:
        "200":
          description: The service process is alive.
          content:
            application/json:
              schema:
                type: object
                required:
                  - status
                properties:
                  status:
                    type: string
                    const: ok
                additionalProperties: false
              example:
                status: ok
```

Define no other path, infrastructure schema, or version field.

- [x] **Step 2: Preserve the future architecture and ordered roadmap without claiming planned work exists**

Create `docs/architecture.md` with these concise sections:

- Current milestone: only `omdi serve`, config, server lifecycle, `/healthz`, packaging, and contracts.
- Target runtime: one Go binary containing HTTP API plus one bounded worker; SQLite-backed persistent queue; disk-backed media; short-lived opaque download tokens.
- Planned boundaries: API, auth, database, downloads, extractor adapters (`yt-dlp`, `gallery-dl`), media inspection/processing (`ffprobe`, `ffmpeg`), security, and worker.
- Planned data flow from iOS Shortcut through API key validation, queued job, extraction, media items, temporary tokens, streaming, and cleanup.
- Security invariants: public allowlisted URLs only; SSRF defenses; argument-based subprocesses; untrusted extractor output; authentication and ownership; secret redaction; filesystem containment; bounded time, size, disk, memory, logs, and concurrency.
- Explicit exclusions: DRM bypass, private media/access-control bypass, cookie management, public signup, web admin, billing, Kubernetes, external queues, object storage, replicas, central hosting, and heavy default transcoding.

Use future tense for every unimplemented component.

Create `docs/roadmap.md` as the authoritative delivery-order document. Preserve the seed's delivery intent while separating this initial repository work into a completed Phase 0:

0. Project foundation: Go and CLI structure; minimal configuration and structured logging; `/healthz`; Compose-first development; pinned tools; quality and security gates; initial OpenAPI, Bruno, and public documentation.
1. Secure core: complete configuration; SQLite migrations/repositories; API-key CLI and authentication; job/item/token domain models; URL validation and SSRF defenses; readiness.
2. Job lifecycle: create, poll, and cancel endpoints; persistent queue and single worker; fake extractor; temporary token generation; streaming download endpoint; cleanup and startup recovery.
3. Real extractors: `yt-dlp`; `ffprobe`; FFmpeg merge/remux; `gallery-dl`; platform/error normalization; optional real smoke-test command.
4. Packaging and documentation finalization: finalize Dockerfile and Compose, OpenAPI, README, Shortcut guide, CI, security, and self-hosting documentation against the complete feature set.

Mark Phase 0 `Complete` and Phases 1–4 `Planned`. State explicitly that Phase 1 has not started and is next, beginning with a Superpowers design and implementation plan for the complete secure-core configuration contract. Make clear that the Phase 0 packaging and documentation baselines do not make Phase 4 active.

- [x] **Step 3: Write the Compose-first README**

`README.md` must include:

- Purpose and current status: foundation milestone with `/healthz` only.
- Prerequisite: Docker with the Compose plugin; optional native Go 1.27.1.
- Quick start: copy `.env.example` to `.env`, run `make compose-up`, call `/healthz`, and stop with `make compose-down`.
- Bruno: open `collection/`, use `local`; copy `collection/.env.example` to `collection/.env` and set a real HTTPS `OMDI_BASE_URL` only for `production`; run `make collection-test`.
- Make target reference for bootstrap, format, tests, coverage, lint, vulnerability checks, build, image scan, run, collection test, and shutdown.
- Selected dependency/tool versions and a manual update procedure: verify official stable releases, update versions/digests/hashes, regenerate the Python lock, run the complete suite, and review vulnerability results. Never suggest floating tags.
- Native commands as optional equivalents.
- Current limitations, current phase, and next development effort linking both `docs/roadmap.md` and `docs/architecture.md`.
- Public-repository warning against committing `.env`, keys, tokens, downloaded media, database files, logs, or real production URLs.
- License link.

- [x] **Step 4: Add private vulnerability reporting guidance**

Create `SECURITY.md` stating that the unreleased `main` branch is currently supported, reports must use GitHub private vulnerability reporting for this repository, reporters must not open a public issue with exploit details, and reports should include impact, affected revision, reproduction, and suggested mitigation without real secrets or personal data.

- [x] **Step 5: Remove the seed with a patch**

Delete `PROJECT_SEED.md` using `apply_patch`. Do not copy the seed wholesale into another file; the maintained roadmap, architecture document, and approved Superpowers spec carry its durable requirements.

- [x] **Step 6: Verify documentation consistency and repository privacy**

Run:

```bash
test ! -e PROJECT_SEED.md
test -f docs/openapi.yaml
test -f docs/roadmap.md
test -f collection/opencollection.yml
rg -n 'Phase 0|Phase 1|Phase 2|Phase 3|Phase 4|next development effort' AGENTS.md README.md docs/roadmap.md
rg -n 'GET /healthz|omdi serve|OMDI_HTTP_ADDR' README.md docs/architecture.md docs/openapi.yaml collection
! rg -n '/home/|/Users/|BEGIN [A-Z ]*PRIVATE KEY|Authorization: Bearer [^<{]|goh_live_|PROJECT_SEED' \
  AGENTS.md CLAUDE.md README.md SECURITY.md docs/architecture.md docs/roadmap.md docs/openapi.yaml collection .env.example
! rg -n '[[:blank:]]+$' --glob '!docker/media-tools.lock' --glob '!.git/**' .
git diff --check
```

Expected: all positive checks succeed and both negative searches return no matches.

- [x] **Step 7: Review checkpoint — do not commit**

Confirm documentation uses English, current behavior is never confused with the roadmap, a zero-context agent can identify the next milestone, OpenAPI and Bruno agree exactly, and the seed is absent. Preserve changes uncommitted.

---

### Task 8: Add CI, run final verification, and prepare the authorized initial commit

**Files:**
- Create: `.github/workflows/ci.yml`
- Modify: `Makefile`
- Modify: documentation only if verification exposes a mismatch

**Interfaces:**
- Consumes: every previous task and the `make ci` contract.
- Produces: a least-privilege public CI workflow and a fully verified, uncommitted initial repository state.

- [x] **Step 1: Create the pinned CI workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
  pull_request:

permissions:
  contents: read

concurrency:
  group: ci-${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  verify:
    runs-on: ubuntu-24.04
    timeout-minutes: 60
    steps:
      - name: Check out repository
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false

      - name: Validate Compose configuration
        run: docker compose config --quiet

      - name: Run all checks
        run: make ci

      - name: Clean up containers
        if: always()
        run: docker compose down --volumes --remove-orphans
```

Do not add write permissions, secrets, artifact uploads, release steps, scheduled jobs, or floating action refs.

- [x] **Step 2: Run the complete verification suite from a clean Compose state**

```bash
docker compose down --volumes --remove-orphans
make ci
docker compose down --volumes --remove-orphans
```

Expected:

- Formatting, vet, GolangCI-Lint, race tests, and coverage pass.
- Coverage is at least 90%.
- govulncheck finds no reachable known Go vulnerability.
- Runtime build succeeds and runs non-root.
- Trivy reports no fixed HIGH or CRITICAL runtime-image vulnerability.
- Compose reaches healthy state.
- Bruno runs one request with all health assertions passing.

- [x] **Step 3: Run final repository hygiene checks**

```bash
docker compose config --quiet
git diff --check
! rg -n '[[:blank:]]+$' --glob '!docker/media-tools.lock' --glob '!.git/**' .
git status --short
test ! -e PROJECT_SEED.md
test -f docs/roadmap.md
git check-ignore -q .env
git check-ignore -q collection/.env
git check-ignore -q .ai-jail
! git check-ignore -q .env.example
! git check-ignore -q collection/.env.example
! rg -n '/home/|/Users/|BEGIN [A-Z ]*PRIVATE KEY|Authorization: Bearer [^<{]|goh_live_' \
  AGENTS.md CLAUDE.md README.md SECURITY.md docs/architecture.md docs/roadmap.md docs/openapi.yaml collection .env.example
```

Inspect `git status --short` manually. It must contain only the intended project files; no `.env`, runtime data, media, cache, database, report, IDE, or agent artifact may be staged or tracked.

- [x] **Step 4: Request explicit commit authorization**

Report the files created/deleted, exact commands run, test/coverage results, dependency versions, Docker/Bruno smoke result, vulnerability results, and any environmental limitation. Ask explicitly whether to create the initial commit. Do not infer commit permission from permission to implement.

- [x] **Step 5: Only if authorized, create one atomic initial commit**

Stage only the reviewed project files; explicitly exclude `.ai-jail`, `.env`, `collection/.env`, runtime output, and any unrelated file. Re-run `git diff --cached --check` and inspect `git diff --cached --stat`.

```bash
git add \
  .dockerignore .env.example .github .gitignore .golangci.yml \
  AGENTS.md CLAUDE.md Dockerfile LICENSE Makefile README.md SECURITY.md \
  cmd collection compose.yaml docker docs go.mod go.sum internal scripts
git diff --cached --check
git diff --cached --stat
git status --short
```

Stop without committing if the status or staged diff contains anything outside the reviewed project files.

Use this conventional commit message:

```text
feat: initialize project foundation
```

Create it without opening an editor:

```bash
git commit -m "feat: initialize project foundation"
```

Do not push.
