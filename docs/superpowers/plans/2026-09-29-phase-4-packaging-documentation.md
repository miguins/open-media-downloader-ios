# Phase 4 — Packaging and Documentation Finalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package the finished service for self-hosting with `compose.yaml` as the production deployment verified in CI, and finalize the self-hosting, Shortcut, README, security, OpenAPI, and roadmap documentation.

**Architecture:** Move the development environment to `compose.development.yaml`, selected by the `Makefile` through `COMPOSE_FILE`, and make `compose.yaml` a production deployment in its own Compose project that builds the existing `runtime` Dockerfile stage with persistent data and the development hardening. Verify it with an isolated smoke script wired into `make ci`. No Go code or HTTP behavior changes.

**Tech Stack:** Docker Compose v2 specification, POSIX shell, the existing `Dockerfile` runtime stage, OpenAPI 3.2.1, Markdown.

**Spec:** `docs/superpowers/specs/2026-09-29-phase-4-packaging-documentation-design.md`

## Global Constraints

- Read the specification and `docs/roadmap.md` before each task; the spec wins over this plan if wording differs.
- Keep documentation provider-neutral: name no hosting provider.
- Never put real API keys, public URLs of real deployments, or links the user shared into the repository.
- Keep all files in English.
- Do not commit or push without explicit user authorization.
- Run `make ci` before handoff when Docker is available.

## Review Focus

- The production project must never share containers, networks, or volumes with the development project; Task 1 pins the project name.
- The production container must keep the read-only root filesystem, dropped capabilities, no-new-privileges, and 1 GiB / 64-PID limits; Task 2 checks the rendered configuration.
- The smoke test must clean up its project and volumes on failure and must not print the created API key; Task 2 pins both.
- Documented commands must match the files and names they reference; Task 4 reviews them.

## File Map

- Rename `compose.yaml` to `compose.development.yaml`.
- Create `compose.yaml` (production), `docker/compose.production-smoke.yaml`, `scripts/production-smoke.sh`, `docs/self-hosting.md`, `docs/shortcut.md`.
- Modify `Dockerfile`, `.env.example`, `Makefile`, `.github/workflows/ci.yml`, `scripts/check-compose.sh`, `scripts/real-smoke.sh`, `README.md`, `SECURITY.md`, `AGENTS.md`, `docs/architecture.md`, `docs/openapi.yaml`, `docs/roadmap.md`.

---

### Task 1: Compose layout, environment example, and image labels

**Files:** `compose.yaml`, `compose.development.yaml`, `.env.example`, `Makefile`, `scripts/real-smoke.sh`, `.github/workflows/ci.yml`, `Dockerfile`, `docs/roadmap.md`

- [ ] Mark Phase 4 `In progress` in the roadmap with a link to the specification and fix the stale Phase 3 workflow sentence.
- [ ] Add OCI labels to the `runtime` stage: `org.opencontainers.image.source`, `licenses`, `title`, and `description`.
- [ ] Rename `compose.yaml` to `compose.development.yaml` with project name `omdi-dev`; export `COMPOSE_FILE ?= compose.development.yaml` from the `Makefile`, point the collection Compose command at it, and default `scripts/real-smoke.sh` to it.
- [ ] Create the production `compose.yaml` with `name: omdi`, one `app` service built from `target: runtime` as `omdi:local`, an optional `.env` `env_file`, the `omdi-data` volume at `/data`, loopback-default ports, the development hardening and limits, `restart: unless-stopped`, and `stop_grace_period: 15s`.
- [ ] Update `.env.example` comments for both files, and validate both files in the CI workflow; clean up the development project there.
- [ ] Verify: both files pass `docker compose config --quiet`, and `make` targets resolve to the `omdi-dev` project while plain `docker compose` resolves to `omdi`.

### Task 2: Production smoke test

**Files:** `scripts/check-compose.sh`, `docker/compose.production-smoke.yaml`, `scripts/production-smoke.sh`, `Makefile`

- [ ] Generalize `scripts/check-compose.sh` to take Compose `-f` arguments, defaulting to the development file, and to also require `read_only: true` and dropped capabilities.
- [ ] Create the smoke override that resets published ports and the `env_file`.
- [ ] Create `scripts/production-smoke.sh`: private temporary environment file, project `omdi-production-smoke`, configuration check, `up --build --detach --wait`, `/healthz` and `/readyz` from inside the container, `omdi keys create` and `omdi keys list` without printing the key, and `down --volumes` in an exit trap.
- [ ] Add `make production-smoke` and run it from `make ci` after `smoke`.
- [ ] Verify: `make production-smoke` passes, and leaves no `omdi-production-smoke` containers or volumes.

### Task 3: Self-hosting and Shortcut guides

**Files:** `docs/self-hosting.md`, `docs/shortcut.md`

- [ ] Write the self-hosting guide sections listed in the specification.
- [ ] Write the Shortcut guide: placeholders, share-sheet input, job creation, the `Repeat`/`Wait` polling pattern, saving by media type, failure reporting with `error_detail`, and a placeholder for the example shortcut link.

### Task 4: README, security, architecture, OpenAPI, and roadmap closure

**Files:** `README.md`, `SECURITY.md`, `docs/architecture.md`, `docs/openapi.yaml`, `docs/roadmap.md`

- [ ] Add a self-hosting section to the README that links both guides, add `make production-smoke` to the command table, and validate the quick start commands.
- [ ] Add deployment notes to `SECURITY.md`.
- [ ] Describe the production Compose packaging in `docs/architecture.md` and update its current milestone.
- [ ] Review `docs/openapi.yaml` against the handlers and set `info.version` to `0.4.0`.
- [ ] Mark Phase 4 `Complete` in the roadmap and state that Phase 5 is next; update the current scope in `AGENTS.md`.

### Task 5: Complete verification

- [ ] Run `make ci` and confirm every step, including `production-smoke`, passes.
- [ ] Confirm that no development or production service was left stopped unexpectedly.
