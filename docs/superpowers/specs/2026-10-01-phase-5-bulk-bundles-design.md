# Phase 5 — Bulk Bundle Downloads Design

## Context and Agreed Direction

Phase 4.1 is complete. Phase 5 lets a user download all media from one succeeded
multi-item job in a single ZIP, including through Safari or a Shortcut file action.
The roadmap requires an opaque, short-lived token as the download credential.
The user approved generating the ZIP in the worker and retaining it until job
cleanup, a dedicated `/v1/bundles/{token}` route, server-generated names, and ZIP
store entries. The contract corrections recorded in the roadmap belong to this phase.

A bundle is the collection of a job's media items, which can include photos and
videos together. ZIP is its download format. Use `Bundle` and `BundleToken` for
domain types and bundle terminology for API schemas, repository/storage methods,
logs, test names, and documentation. ZIP terminology describes the file format
and its encoding rules.

This specification makes the remaining lifecycle, storage, and compatibility
decisions explicit for review before the implementation plan is written.

## Goals and Scope

- Newly completed jobs with two or more items have one complete persisted ZIP.
- Authenticated polling exposes `bundle` beside `items`, with its own temporary
  URL, expiry, file name, media type, and actual size.
- Bundle links work without an API-key header and support full downloads,
  headers-only requests, and byte ranges.
- Bundle creation is bounded by item, byte, time, memory, and disk budgets and
  participates in cancellation, deletion, startup recovery, and retention cleanup.
- Individual downloads remain available, including for single-item jobs.
- Authentication JSON errors send `Cache-Control: no-store`; the item-download
  contract documents and tests unsatisfiable ranges.

No bundle is built on an HTTP request. There is no separate bundle queue,
configurable compression, additional runtime tool, or bundle of multiple jobs.
Previously succeeded jobs are not backfilled during migration or polling.

## Alternatives and Decision

Persisting the ZIP in the single worker costs approximately another copy of the
media and adds a bounded preparation step before success. It gives the HTTP layer
a complete seekable file with a known size and avoids work per download. This is
the selected approach.

Streaming a ZIP on demand saves retained disk space but complicates length and
range handling and repeats bundle work. Lazily persisting it on the first download
would require request-time coordination, disk admission, and separate failure
handling. Neither is included in this phase.

The dedicated bundle route keeps item tokens and bundle tokens separate without
changing the existing item-token schema. An unauthenticated job ID is never a
download credential; an authenticated bundle endpoint would require a header
that cannot accompany a link handed to Safari or a Shortcut file action.

## Package Boundaries and Persistence

Extend the existing packages rather than introduce a new subsystem:

- `internal/job`: bundle metadata and bundle-token domain values.
- `internal/storage`: safe bundle construction, installation, opening, and limits.
- `internal/store`: bundle and token persistence and atomic job completion.
- `internal/worker`: bundle creation within the existing job lifecycle.
- `internal/api`: polling response and bundle GET/HEAD handlers.
- `internal/cleanup`: expired bundle tokens and interrupted-job file recovery.
- `internal/auth`: the JSON response cache correction; existing opaque-token
  generation and hashing are reused.

A new SQLite migration adds `bundles` and `bundle_tokens`. A bundle is keyed
by `job_id`, with a cascading foreign key to `jobs`, and stores `file_name`,
`size_bytes`, and `created_at`. Its media type is always `application/zip`.
Bundle sizes must be positive. Tokens store only the hash, bundle job ID,
owner ID, creation time, and expiry. A unique bundle job ID in the token table
keeps at most one current token per bundle; expiry is indexed for cleanup.

Repository operations enforce that token ownership matches the bundle's job
owner. Token lookup validates the bundle/job relationship and job retention.
The bundle token cannot authorize an item download, or vice versa, because the
routes query separate token tables.

Job completion commits the status, items, and optional bundle metadata in one
transaction. New worker completions require bundle metadata for multi-item jobs
and omit it for single-item jobs. Schema migration preserves existing jobs,
items, and download tokens. An existing succeeded multi-item job without bundle
metadata remains downloadable individually and omits `bundle`.

## Worker and File Lifecycle

1. Claim and admit the job using the existing single-worker queue.
2. Extract and ingest media under the existing output validation rules.
3. Remove the extractor work directory after successful ingestion, before copying
   media into the ZIP, so remux leftovers do not overlap bundle disk usage.
4. For two or more items, check bundle disk headroom and build a private temporary
   file inside the job directory. Copy items in position order, finish the ZIP,
   verify its final size, close it, and rename it to the fixed internal name
   `bundle.zip`. Single-item jobs skip this step.
5. Check cancellation and commit succeeded status, items, and bundle metadata.
6. On a canceled/deleted job or failed database completion, discard its files.

Bundle construction receives the job context. The existing timeout and
cancellation watcher remain active through ingestion, ZIP generation, and the
completion boundary. Copying checks cancellation between bounded reads; success
is never committed after the job context has expired or been canceled. Database
completion still checks that the persisted job is running, protecting against
cancel/delete races after the last context check. Failure recording and removal
use an appropriate still-live lifecycle context rather than the expired job context.

A ZIP is required for a new multi-item job to succeed. Exceeding item, media,
bundle-size, or disk-admission limits fails with `too_large`; timeout fails with
`timeout`; bundle filesystem/write/close failures fail with `internal`. Unsafe
media inputs use the existing invalid-output failure classification. No new
public error-detail values are needed. There is no bundle retry or partial
success. All construction failures remove partial bundles and ingested files;
cleanup failures are logged safely and retried through recovery/retention cleanup.

On process shutdown, the worker leaves no succeeded record for unfinished work.
Startup recovery marks interrupted running jobs failed, clears work directories,
and removes job directories belonging to failed or canceled jobs as well as
orphaned directories. Recovery runs before the worker and fails startup if its
required cleanup cannot complete. This covers a crash after ingestion, during ZIP
writing, after installation, or before database commit. Succeeded directories,
including legacy ones, are preserved.

Job expiry and operator deletion/purge remove the entire job directory, including
the ZIP. Foreign-key cascades delete bundle metadata and tokens when jobs are
deleted, including revoked-key purge. Expired bundle tokens are removed during
the existing periodic sweep. Expiring or replacing a token does not delete the
bundle while its job is retained.

## ZIP Format, Names, and Resource Bounds

The downloaded name is `omdi-<first-eight-job-id-characters>.zip`, matching the
existing item-name convention. Entries use each item's server-generated
`file_name`, in ascending position order, at the bundle root. Entry names must
be unique plain basenames with no separators, special path components, or
control characters, and at most 128 bytes. Remote titles and extractor paths
never become bundle names, comments, or extra fields. Entries contain only the
validated media bytes; there is no manifest or source-URL metadata.

Use the Go standard library ZIP writer with the store method for every entry.
ZIP64 is supported because the configured media budget can exceed 4 GiB. Entry
timestamps derive from item creation time, and the file download timestamp derives
from bundle creation time. Do not preserve source permissions or comments.

Let `B = OMDI_MAX_JOB_BYTES`, `N = OMDI_MAX_JOB_ITEMS`, and
`H(n) = 1024 * (n + 1)` bytes for `n` entries. The bundle must satisfy:

- `2 <= n <= N` and total media size at most `B`.
- Actual ZIP size at most `total media size + H(n)`, and therefore at most
  `B + H(N)`. This deliberately conservative bound includes central-directory,
  per-entry, descriptor, and ZIP64 overhead under the restricted naming and
  metadata rules. A capped writer enforces it while writing, including closing
  the central directory; final validation checks it again.
- Initial disk admission requires at least
  `OMDI_MIN_FREE_BYTES + 2 * B + H(N)` free bytes, covering the larger of remux
  input/output overlap and media/bundle overlap after work-directory removal.
- Immediately before bundle construction, available space must be at least
  `OMDI_MIN_FREE_BYTES + total media size + H(n)`.

These are admission checks rather than a filesystem reservation. Concurrent
external disk consumption can still cause write failure; the job then fails
closed and discards its files. No additional configuration is introduced.

Bundle construction copies through a fixed-size buffer and retains only bounded
per-entry metadata. It never loads the ZIP or an entire media file into memory.
Source files are opened through the storage boundary without following symlinks,
must be regular single-link files, and must match recorded sizes. Copying is
bounded by each recorded size and the aggregate budget; growth, truncation, or
unexpected metadata fails closed. Private directories use mode `0700`; bundle
files use `0600`. Temporary creation is exclusive. Bundle opening validates the
job ID, uses the fixed internal basename, rejects symlinks/nonregular or
multiple-link files, and checks the recorded bundle size before serving.

## HTTP Contract and Tokens

For a succeeded job with bundle metadata, `GET /v1/jobs/{id}` includes:

```json
{
  "bundle": {
    "file_name": "omdi-abcdefgh.zip",
    "media_type": "application/zip",
    "size_bytes": 123456,
    "download_url": "https://media.example/v1/bundles/<opaque-token>",
    "download_expires_at": "2026-10-01T15:15:00Z"
  }
}
```

The example is the bundle portion of the job response. No bundle ID or internal
path is exposed. `bundle` is omitted for non-succeeded jobs, single-item jobs,
and legacy jobs without bundle metadata. A succeeded new multi-item job never
silently falls back to omitting an existing bundle because token issuance or
metadata retrieval failed; those failures produce the existing safe JSON `500`.

Polling replaces the bundle token just as it replaces item tokens. Its expiry is
the earlier of `now + OMDI_TOKEN_TTL` and job expiry. Token generation uses the
existing 32-byte random token format and persists only its hash. A bundle token
is reusable until replacement or expiry, allowing HEAD and Range requests.
Bundle and item token lifetimes are independent. API-key revocation prevents
further authenticated polling; already-issued tokens retain the existing item
download semantics and remain usable until replacement, expiry, or job/key purge.

`GET /v1/bundles/{token}` streams the persisted file without an Authorization
header. `HEAD` validates the same credential and returns the full download headers
without a body. Responses include `Content-Type: application/zip`, a safe
attachment `Content-Disposition`, `Content-Length`, `Accept-Ranges: bytes`,
`Cache-Control: no-store`, `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer`, and the request trace ID. Serving reuses the
existing content-serving and rolling write-deadline behavior.

GET returns `200` for a full download, `206` for satisfiable ranges, and `416`
with `Content-Range: bytes */<bundle-size>` for an unsatisfiable range. HEAD
ignores Range and returns `200` for a valid bundle. Conditional request behavior
follows the existing item endpoint. Malformed, unknown, replaced, expired,
wrong-route, or deleted-resource tokens and unavailable bundle files all return
the same safe `404`; HEAD errors have no body. Following the approved contract
refinement, `416` uses the standard JSON error body with code
`range_not_satisfiable`, preserving `Content-Range` and privacy headers.

Trace logs use the route pattern, fixed reason codes, job/key IDs, item counts,
sizes, and durations. They never include tokens, request URLs, source names,
internal paths, bundle contents, or raw filesystem errors.

## Contract Corrections and Documentation

- Add `Cache-Control: no-store` to authentication `401` and `500` JSON responses
  and declare it on the OpenAPI `Unauthorized` response.
- Declare item-download GET `416` and `Content-Range: bytes */<size>` and pin the
  existing behavior with an HTTP test. Preserve safe download headers on errors.
- Add bundle GET/HEAD paths, the optional job bundle schema, and actual response
  headers/statuses to `docs/openapi.yaml`. Set the contract version to `0.5.0`.
- Add `collection/bundles/` with organized requests named `get-bundle` and
  `head-bundle`, assertions, and bounded multi-item polling. Preserve the
  existing single-item flow. Extend the build-tagged fake with a deterministic
  multi-item fixture selected by a fixed approved public-post URL; no runtime
  fake switch or production-only fixture handling is introduced.
- Update README, architecture, self-hosting capacity notes, Shortcut instructions,
  security notes where needed, and project scope. Explain bundle link replacement,
  token-only access, legacy behavior, and approximately doubled retained media
  usage. Update the roadmap when implementation begins and when delivery completes.

## Validation and Acceptance Criteria

All behavior changes follow TDD with tests beside the existing packages. Required
coverage remains at least 90% overall and 100% for the existing security-critical
packages, including the added bundle storage and authentication paths.

Tests must demonstrate:

- Migration of populated databases preserves existing media, jobs, and tokens.
- Atomic completion with bundle metadata, rollback, ownership enforcement,
  cancellation/deletion conflicts, and cascading bundle/token deletion.
- A valid ZIP opens with a ZIP reader, contains exactly the ordered media entries,
  uses store compression, and yields the original bytes and matching sizes.
- Single-item and legacy jobs omit `bundle`; new multi-item jobs expose it.
- Bundle tokens rotate, expire at the job boundary, cannot cross routes or
  owners, and follow documented revoke/purge semantics.
- Full GET, HEAD, partial GET, unsatisfiable ranges, safe headers, uniform errors,
  missing files, and logs that do not disclose credentials or paths.
- Unsafe names/files, duplicate names, item/byte/overhead limits, source size
  changes, insufficient disk, write/finalization failures, cancellation, timeout,
  and shutdown cannot expose a partial bundle or leave a succeeded partial job.
- Recovery removes interrupted-job artifacts; retention cleanup and CLI
  deletion/purge remove the ZIP and its tokens while fresh succeeded jobs remain.
- Authentication `401` and storage-error `500` JSON responses are not cacheable,
  and the existing item endpoint's unsatisfiable range behavior is documented
  and tested.

Use bounded fixtures and fault injection for large-size/ZIP64 and resource-limit
branches without allocating real multi-gigabyte media. The complete offline Bruno
collection covers the bundle HTTP flow. Run the appropriate Makefile checks and
the complete `make ci` before implementation handoff when Docker is available;
restore the local service if the verification targets stopped a running instance.

## Delivery Gates

The user reviews and approves this written specification and explicitly authorizes
its commit. Then prepare a current-format Superpowers implementation plan for
review, approval, commit authorization, and execution-method selection. Product
behavior changes start only after those gates. This document alone does not mark
Phase 5 implemented or change its delivery status.
