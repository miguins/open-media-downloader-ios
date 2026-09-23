# Phase 2 — Job Lifecycle Design

## Context

Phase 1 delivered configuration, SQLite persistence, API keys with bearer authentication middleware, job/item/token domain models, URL policy, and `/readyz` ([design](2026-09-22-phase-1-secure-core-design.md)). Phase 2 makes the service usable end to end with a fake extractor: an authenticated client submits a URL, polls the job, receives temporary download links, and streams the files. Real extractors arrive in Phase 3 behind the extractor boundary defined here.

## Goals

- Authenticated endpoints to create, poll, and cancel jobs, mounting the Phase 1 middleware.
- A persistent SQLite-backed queue processed by one bounded worker.
- A fake extractor that produces synthetic media through the same boundary Phase 3 adapters implement.
- Short-lived opaque download tokens and a streaming download endpoint.
- Periodic cleanup of expired tokens, jobs, and files, plus startup recovery.
- `omdi jobs list`, `omdi jobs delete`, and `omdi jobs purge` for operator management.

## Non-Goals

- `yt-dlp`, `gallery-dl`, `ffprobe`, and `ffmpeg` invocation, and extractor egress control (Phase 3).
- Request rate limiting beyond the per-key queue limit.
- Admin HTTP endpoints.

## Package Boundaries

| Package | Change |
| --- | --- |
| `internal/api` | Job and download handlers; JSON decoding and error responses. |
| `internal/auth` | Adds download-token generation and hashing beside API keys. |
| `internal/storage` | New. Data-directory layout, private job and work directories, ingestion of extractor output, and removal. Security-critical: 100% coverage. |
| `internal/worker` | New. Queue claiming, execution with timeout and cancellation, and the `Extractor` interface it consumes. |
| `internal/extractor` | New. The `Fake` extractor; Phase 3 adds real adapters here. |
| `internal/cleanup` | New. Periodic removal of expired tokens, jobs, and orphaned files; startup recovery. |
| `internal/store` | Adds claim, completion, listing, deletion, and expiry queries. |
| `internal/config` | Adds `OMDI_PUBLIC_URL` and `OMDI_MAX_QUEUED_JOBS`, and a retention rule. |
| `internal/app` | Runs the worker and cleanup loops beside the HTTP server under one lifecycle. |
| `cmd/omdi` | Wires the new components; adds `jobs list`, `jobs delete`, and `jobs purge`. |

`Extractor` is the one new interface; it is a real substitution boundary (fake now, real adapters in Phase 3).

## Configuration Additions

| Variable | Default | Rule |
| --- | --- | --- |
| `OMDI_PUBLIC_URL` | `http://localhost:8080` | Absolute `http` or `https` URL without user info, query, or fragment; a trailing slash is removed. Used to build download links; the request `Host` header is never trusted. |
| `OMDI_MAX_QUEUED_JOBS` | `10` | Integer 1–1000. Maximum queued plus running jobs per API key. |

New cross-field rule: `OMDI_JOB_RETENTION` must exceed `OMDI_JOB_TIMEOUT`, so a job cannot expire while it may still run.

## HTTP API

All `/v1` job endpoints require `Authorization: Bearer <key>`. Requests with bodies require `Content-Type: application/json`, are limited to `OMDI_MAX_REQUEST_BYTES`, and must contain exactly one JSON object without unknown fields. Errors are `{"error":"<code>"}` with a fixed code set:

| Status | Code |
| --- | --- |
| 400 | `invalid_request` |
| 401 | `unauthorized` |
| 404 | `not_found` (also for jobs owned by another key) |
| 409 | `conflict` |
| 413 | `request_too_large` |
| 415 | `unsupported_media_type` |
| 422 | `unsupported_url` |
| 429 | `too_many_jobs` |
| 500 | `internal` |

### `POST /v1/jobs`

Request `{"url":"https://..."}`. The URL passes `urlpolicy.Normalize`. When the key already has `OMDI_MAX_QUEUED_JOBS` queued or running jobs, the response is `429`. Otherwise the job is stored as `queued`, the worker is notified, and the response is `202` with the job representation and a `Location: /v1/jobs/{id}` header.

### `GET /v1/jobs/{id}`

Returns `200` with:

```json
{
  "id": "…",
  "status": "succeeded",
  "url": "https://vimeo.com/123",
  "platform": "vimeo",
  "error": null,
  "created_at": "2026-09-23T12:00:00Z",
  "updated_at": "2026-09-23T12:00:05Z",
  "expires_at": "2026-09-24T12:00:00Z",
  "items": [
    {
      "id": "…",
      "file_name": "omdi-abcdefgh-1.mp4",
      "media_type": "video/mp4",
      "size_bytes": 1024,
      "download_url": "https://host/v1/downloads/<token>",
      "download_expires_at": "2026-09-23T12:15:05Z"
    }
  ]
}
```

`items` is present only for `succeeded` jobs. Each poll of a succeeded job issues a fresh token per item and deletes that item's earlier tokens, so at most one live token exists per item and repeated polling cannot grow the token table. The Shortcut downloads from the links in its latest poll. `error` is one of the Phase 1 error codes or `null`. Responses carry `Cache-Control: no-store`.

### `DELETE /v1/jobs/{id}`

Cancels a queued or running job and returns `200` with the job. Canceling an already canceled job returns `200` unchanged. Canceling a succeeded or failed job returns `409`. Cancellation is recorded in the database; the worker running the job observes it within one second, cancels its context, and discards its partial files.

### `GET /v1/downloads/{token}` and `HEAD`

Unauthenticated: the token is the credential. The token is 32 random bytes encoded as unpadded base64url; only its SHA-256 is stored. Malformed, unknown, and expired tokens all return the same `404`. A valid token streams the item with `http.ServeContent` (supporting `Range` and `HEAD`) and these headers:

- `Content-Type` from the stored media type; `X-Content-Type-Options: nosniff`.
- `Content-Disposition: attachment` with the server-generated file name.
- `Cache-Control: no-store`, `Referrer-Policy: no-referrer`.

Tokens stay valid until expiry so interrupted downloads can resume. The file is opened without following symlinks and must be a regular file inside the job directory. The server's 15-second write timeout would cut off large files, so the download handler extends the write deadline by 30 seconds after each successful write through `http.ResponseController`; a stalled client is still disconnected.

## Storage Layout

```text
$OMDI_DATA_DIR/
├── omdi.db
├── jobs/<job-id>/<item-id>   final media, 0600 files in 0700 directories
└── work/<job-id>/            extractor scratch space, removed after every run
```

`internal/storage` builds every path from validated IDs only (`id.Valid`), never from extractor output or user input. After extraction, `Ingest` accepts only plain entry names reported by the extractor. It moves each entry into the private job directory first and then validates the destination, which the extractor cannot reach, so the file cannot be swapped between check and use. The destination must be a non-empty regular file (checked with `Lstat`; symlinks, directories, devices, and FIFOs fail the job) with exactly one link, which rejects hard links to files such as the database, and the total must stay within the size budget. Stored files are later opened with `O_NOFOLLOW`. The total size must not exceed `OMDI_MAX_JOB_BYTES`; the extractor also receives the budget. Accepted files are moved into `jobs/<job-id>/<item-id>`. Extractor-provided names are discarded.

The media type comes from the extractor and must be in an allowlist (`video/mp4`, `video/webm`, `video/quicktime`, `image/jpeg`, `image/png`, `image/webp`, `image/gif`, `audio/mp4`, `audio/mpeg`). The allowlist maps each type to the extension used in the generated download name `omdi-<first 8 job-ID chars>-<position>.<ext>`.

Before a job starts, the worker requires `OMDI_MIN_FREE_BYTES` of free space plus the job budget; otherwise the job fails with `too_large`.

## Worker

- One goroutine. It wakes on a notification from `POST /v1/jobs` and on a 5-second ticker as a fallback.
- It claims the oldest queued job with a single `UPDATE … WHERE id = (SELECT … LIMIT 1) AND status = 'queued' RETURNING …`. That statement makes claiming atomic even if a future version runs more workers.
- Each job runs under `context.WithTimeout(OMDI_JOB_TIMEOUT)`. While it runs, the worker checks the job row every second and cancels the context when the row is no longer `running` or no longer exists. The database is the only cancellation channel, so the API and the separate CLI process (`jobs purge`) stop work the same way.
- The worker records the outcome with the Phase 1 `from` status check, so a race between completion and cancellation has one winner. The loser removes its files.
- Success is committed atomically: items and the `succeeded` status are written in one transaction.
- Failures map to error codes: timeout → `timeout`, budget exceeded → `too_large`, extractor error → `extraction_failed`, anything else → `internal`. Details are logged with the job ID and never returned or stored.

```go
type Extractor interface {
    Extract(ctx context.Context, request extractor.Request) ([]extractor.File, error)
}
// Request{URL, Platform, WorkDir string; MaxBytes int64}
// File{Name string /* entry inside WorkDir */; MediaType string}
```

`extractor.Fake` writes one small synthetic file named from the job's position with type `video/mp4`, honors cancellation, and never touches the network. It is the only extractor wired in Phase 2.

## Cleanup and Recovery

On startup, before the HTTP server accepts requests:

- Jobs left `running` are marked `failed` with `internal`. They are not re-queued, so a URL that crashes the process cannot loop.
- `work/` is emptied.
- Directories under `jobs/` without a matching job row are removed.

Every minute, the cleanup loop:

- Deletes expired download tokens.
- Deletes terminal jobs past `expires_at`, removing the job directory before the row. Items and tokens cascade.
- Removes job directories without a matching row, which covers a worker that finished moving files just after its job was deleted.

## CLI

```text
omdi jobs list [--owner <key-id>] [--status <status>]   # id, owner, platform, status, created, expires
omdi jobs delete <id>                                   # removes the job, its files, and tokens
omdi jobs purge --yes [--owner <key-id>]                # removes every job, including running ones
```

`jobs delete` refuses running jobs, which must be canceled through the API or allowed to finish. The refusal is enforced in SQL (`DELETE … WHERE status <> 'running'`). A deleted queued job cannot be claimed, and a worker that completes a job deleted concurrently gets `not found` and removes its files. The URL is not printed by `list`.

`jobs purge` is the complete cleanup. It requires `--yes`, and `--owner` limits it to one key. It runs these steps:

1. Mark every queued and running job in scope `canceled`. A worker running one of them stops within a second.
2. Delete the rows. Items and tokens cascade.
3. Remove the job directories.
4. Report how many jobs were removed.

A worker that is interrupted mid-run finds its job gone and removes its own files. The periodic orphan sweep covers any directory created in the gap. Make targets `job-list`, `job-delete ID=…`, and `job-purge` (optionally `OWNER=…`) mirror the key targets.

## Contracts

- OpenAPI `0.2.0`: the three job operations, the download operation, the bearer security scheme, the job and error schemas, and every status code above.
- Bruno:
  - `jobs/` folder: `jobs` (POST `/v1/jobs`), `get-job` (polls with `bru.sleep` and `bru.setNextRequest` until the job finishes and stores the download token), and `cancel-job`. Because the fake extractor finishes almost immediately, `cancel-job` verifies the `409 conflict` for the succeeded job; canceling active jobs is covered by Go tests.
  - `downloads/` folder: `downloads` for the download request.
  - Environments read the key from `{{process.env.OMDI_API_KEY}}`.
  - `make collection-test` creates a throwaway key in the Compose volume, passes it to the Bruno container through the environment, and revokes it afterwards.

## Testing

- TDD throughout. `internal/auth`, `internal/urlpolicy`, and `internal/storage` require 100% coverage; total at least 90%.
- **Storage:** symlink, directory, FIFO, oversize, traversal-shaped name, and permission cases.
- **Worker:** stub extractors cover success, error, timeout, cancellation before and during a run (through the database), deletion during a run, the completion/cancellation race, and budget overrun.
- **API:** every status code, cross-owner `404`, the one-live-token rule, `Range` and `HEAD` downloads, and the extended write deadline.
- **Cleanup and recovery:** tests against a temporary data directory.

## Delivery Order

1. Configuration additions.
2. Store queries (claim, complete, list, delete, expiry, token replacement).
3. Download tokens in `internal/auth`.
4. `internal/storage`.
5. `internal/extractor` fake and `internal/worker`.
6. `internal/cleanup` and startup recovery; `internal/app` background loops.
7. Job endpoints with OpenAPI and Bruno.
8. Download endpoint with OpenAPI and Bruno.
9. `omdi jobs list|delete|purge` CLI and Make targets.
10. Coverage gate, documentation, roadmap, and `make ci`.
