# Phase 4.1 — Releases and Published Images Design

## Context

Phase 4 packaged the service for self-hosting: `compose.yaml` builds the hardened
`runtime` image from source, and CI verifies it. Building compiles FFmpeg and takes
several minutes, and platforms that deploy a prebuilt image cannot use the
repository at all without a build step of their own. The project also has no
versioned releases: the only version is `info.version` in `docs/openapi.yaml`.

Phase 4.1 publishes each version as a GitHub release with a multi-architecture
image in the GitHub Container Registry. It is a continuation of Phase 4 packaging,
numbered so that Phase 5 keeps its number. It changes no application behavior.

## Goals

- Publish a release for every semantic version tag, and only after the complete
  `make ci` suite has passed for the tagged commit on `main`.
- Publish the `runtime` image for `linux/amd64` and `linux/arm64` under the exact
  version, with a signed build provenance attestation.
- Keep one version number: the tag, the image tag, and the OpenAPI version match.
- Lint GitHub Actions workflows in CI, because the release workflow cannot run
  outside a tag push.
- Document deploying the published image and verifying its provenance.

## Non-Goals

- Floating image tags such as `latest`, `0`, or `0.4`. Deployers choose an exact
  version, consistent with the project's pinned-version policy.
- Changing `compose.yaml` to use the published image. It keeps building from source,
  so it always runs, and CI always tests, the checked-out code.
- Release binaries, installers, or packages other than the container image.
- Automated version bumps, changelog files, or release branches.
- Publishing images for commits without a version tag.

## Versioning

- Tags have the form `vMAJOR.MINOR.PATCH` with no leading zeros, prerelease, or build
  suffixes. The workflow's tag filter selects candidates, and
  `scripts/check-release-version.sh` enforces the exact form and requires the version
  to equal `info.version` in `docs/openapi.yaml`, so the contract is bumped before
  tagging.
- The first release is `v0.4.0`, the current OpenAPI version.
- Releasing is: raise `info.version` in a commit, push it, then push the matching tag.

## Release Workflow

`.github/workflows/release.yml` runs on pushed tags matching
`v[0-9]+.[0-9]+.[0-9]+`. The workflow-level token has no permissions; each job
requests only what it needs. Releases for the same tag never run concurrently or
cancel each other. Actions are pinned to full commit SHAs of their newest stable
releases, and only first-party actions are used: `actions/checkout`,
`docker/setup-buildx-action`, and `actions/attest`. Registry login and release
creation use the `docker` and `gh` CLIs preinstalled on GitHub-hosted runners.

1. **verify** (`contents: read`, `actions: read`): check out the tag, run the version
   check, and run `scripts/check-release-ci.sh`, which requires the tagged commit to
   be on `main` and the CI workflow to have succeeded for it on a push to `main`,
   waiting while CI is queued or running. It outputs the version. Running `make ci`
   again would repeat the same checks on the same commit; `ci.yml` also runs only
   for branches, so a tag push starts no CI run.
2. **image-amd64** on `ubuntu-24.04` and **image-arm64** on `ubuntu-24.04-arm`
   (`contents: read`, `packages: write`): build the `runtime` target natively for
   one platform with `docker buildx build`, add
   `org.opencontainers.image.version` and `org.opencontainers.image.revision`
   labels, and push it by digest only, without a tag. BuildKit's own provenance is
   disabled, because the attestation below covers the published image. Each job
   outputs its digest. Native runners avoid compiling FFmpeg under emulation.
3. **publish** (`contents: write`, `packages: write`, `id-token: write`,
   `attestations: write`): combine both digests into one manifest list tagged with
   the version, attest the manifest list's digest with `actions/attest`, pushing the
   attestation to the registry, and create the GitHub release with generated notes
   prepended by the image reference and digest. Artifact metadata storage records
   are disabled, because they require an organization-owned repository.

The image is `ghcr.io/miguins/open-media-downloader-ios:<version>`. The package is
created on the first release; its visibility must be confirmed as public once in the
package settings.

## Workflow Linting

`make workflow-lint` runs `actionlint`, pinned by version and digest, over
`.github/workflows/`, including the shell scripts in `run` steps through the
bundled ShellCheck. `make ci` runs it. `scripts/test-check-release-version.sh`
tests the version check against valid, malformed, prefixed, suffixed, and
mismatched tags, and `scripts/test-check-release-ci.sh` tests the CI gate with a
fake `gh` against passing, pending, failed, canceled, timed-out, and off-`main`
cases. `make ci` runs both next to the real-smoke script test.

## Documentation

- `docs/self-hosting.md`: a prebuilt-image section with a `compose.override.yaml`
  that replaces the build with an exact image version while keeping every other
  setting, deployment on platforms that run a prebuilt image, and provenance
  verification with `gh attestation verify`. `.gitignore` ignores
  `compose.override.yaml`, so updating the checkout never conflicts with it.
- `README.md`: a short Releases section, the new `make` targets, and the release
  procedure.
- `SECURITY.md`: only the latest release is supported, fixes land on `main` and ship
  in the next release, and the published image's provenance can be verified.
- `docs/architecture.md`: the release pipeline in the Packaging section.
- `AGENTS.md`: the current scope.
- `docs/roadmap.md`: Phase 4.1 complete at the end.

## Testing

- `make workflow-lint` over both workflows.
- `scripts/test-check-release-version.sh`.
- The complete `make ci` suite.
- The release workflow itself can run only on a real tag push; the first release,
  `v0.4.0`, is its end-to-end test, followed by pulling the image and verifying its
  attestation.

## Delivery Order

1. Specification, plan, and roadmap entry.
2. Version check script and its test.
3. Workflow linting.
4. Release workflow.
5. Documentation and roadmap closure.
6. Complete verification with `make ci`.
