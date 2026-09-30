# Phase 4.1 — Releases and Published Images Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish each semantic version as a GitHub release with a multi-architecture runtime image and a signed provenance attestation, after the complete CI suite passes.

**Architecture:** A tag-triggered workflow verifies the tag against the OpenAPI version, runs `make ci`, builds each platform natively and pushes it by digest, then combines the digests into one versioned manifest list, attests it, and creates the release. `actionlint` guards the workflows in CI.

**Tech Stack:** GitHub Actions, `actions/checkout` 7.0.1, `docker/setup-buildx-action` 4.4.1, `actions/attest` 4.2.2, Docker Buildx, GitHub CLI, actionlint 1.7.12, POSIX shell.

**Spec:** `docs/superpowers/specs/2026-09-30-phase-4-1-releases-design.md`

## Global Constraints

- Read the specification and `docs/roadmap.md` before each task; the spec wins over this plan if wording differs.
- Pin every action to the full commit SHA of its newest stable release, and every image to a version and digest.
- Grant each job only the permissions the specification lists; the workflow-level token has none.
- Never publish a floating tag.
- Keep documentation provider-neutral and in English.
- Do not commit or push without explicit user authorization.
- Run `make ci` before handoff when Docker is available.

## Review Focus

- A tag that is not exactly `vMAJOR.MINOR.PATCH`, or that differs from the OpenAPI version, must stop the release before anything is published; Task 1 pins these cases.
- Nothing may be pushed before `make ci` passes on the tagged commit; Task 3 orders the jobs.
- The attestation must cover the digest of the published manifest list, not a per-platform image; Task 3 pins the subject.

## File Map

- Create `scripts/check-release-version.sh`, `scripts/test-check-release-version.sh`, `.github/workflows/release.yml`.
- Modify `Makefile`, `README.md`, `SECURITY.md`, `AGENTS.md`, `docs/self-hosting.md`, `docs/architecture.md`, `docs/roadmap.md`.

---

### Task 1: Version check

- [ ] Write `scripts/test-check-release-version.sh` covering a matching tag, a mismatched version, a missing `v`, leading zeros, prerelease and build suffixes, and extra components; observe it fail without the script.
- [ ] Write `scripts/check-release-version.sh <tag>`: validate the exact form, compare with `info.version` in `docs/openapi.yaml`, and print `version=<version>`; failures print a fixed message and exit nonzero.
- [ ] Run the test from `make ci`.

### Task 2: Workflow linting

- [ ] Add `make workflow-lint`, running the pinned `actionlint` image read-only over the repository, and run it from `make ci`.
- [ ] Verify it passes on the existing CI workflow.

### Task 3: Release workflow

- [ ] Create `.github/workflows/release.yml` with the `verify`, `image-amd64`, `image-arm64`, and `publish` jobs and the permissions, concurrency, labels, digest outputs, manifest list, attestation, and release described in the specification.
- [ ] Verify it with `make workflow-lint`.

### Task 4: Documentation and closure

- [ ] Document prebuilt-image deployment and provenance verification in `docs/self-hosting.md`.
- [ ] Add a Releases section, the release procedure, and the new targets to the README.
- [ ] Update supported versions and provenance in `SECURITY.md`, the Packaging section of `docs/architecture.md`, and the scope in `AGENTS.md`.
- [ ] Mark Phase 4.1 `Complete` in the roadmap and state that Phase 5 is next.

### Task 5: Complete verification

- [ ] Run `make ci` and confirm every step passes.
