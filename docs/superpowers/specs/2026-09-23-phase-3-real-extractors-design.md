# Phase 3 — Real Extractors Design

## Context

Phase 2 delivered the complete job lifecycle with a fake extractor behind the
`worker.Extractor` boundary. Phase 3 keeps that lifecycle and replaces the
production fake with bounded real extraction for one public media post per job.
The phase remains one roadmap phase, delivered through four internal milestones:

1. `yt-dlp` and extractor egress control.
2. `ffprobe` inspection and FFmpeg merge/remux behavior.
3. `gallery-dl`, bounded carousels, and final platform routing.
4. An opt-in real smoke test, documentation, and Phase 3 closure.

Phase 3 stays `In progress` until all four milestones are delivered. It does not
insert a new roadmap phase or renumber Phase 4.

## Goals

- Download public media from one supported post per job with real extractors.
- Preserve the existing one-URL request, queue, ownership, cancellation,
  download-token, cleanup, and storage contracts.
- Route each platform deterministically to `yt-dlp` or `gallery-dl` without
  cross-adapter fallback.
- Permit multiple output files only when they belong to the submitted post,
  such as an image carousel.
- Prefer media that plays on iOS without heavy transcoding.
- Validate every extractor network destination, including redirects and media
  CDN hosts, before connecting.
- Bound execution time, item count, network transfer, filesystem output,
  diagnostics, memory, and child-process count.
- Normalize tool failures without exposing submitted URLs, remote responses,
  credentials, internal paths, or raw diagnostics.
- Retain deterministic offline unit, integration, Bruno, and CI coverage.

## Non-Goals

- Batch submission. Multiple URLs remain multiple `POST /v1/jobs` requests and
  create independent jobs.
- Deduplication. Repeated submissions of the same URL create distinct jobs.
- Playlists, profiles, channels, collections, feeds, or recursively paginated
  galleries.
- Private media, cookies, account sessions, `.netrc`, authentication files, or
  access-control bypasses.
- DRM circumvention.
- Live-stream recording.
- Heavy or quality-changing transcoding.
- Automatic fallback from one extractor to another.
- Kernel-level containment of a compromised media-tool binary. The pinned and
  verified tool binaries are trusted; submitted URLs, remote data, diagnostics,
  metadata, filenames, and output files are not.
- Running real platform downloads in the required CI suite.

## Product Behavior

`POST /v1/jobs` continues to accept exactly one URL. A JSON array or additional
request fields remain `400 invalid_request`. Clients submit several URLs as
several requests, subject to the existing per-key active-job limit. The single
worker continues to process jobs serially.

A job represents one post. One video post normally produces one item; a post
containing a bounded carousel may produce several items in source order. The
new `OMDI_MAX_JOB_ITEMS` setting defaults to 20 and accepts values from 1 to
100. Discovery of item `max + 1` rejects the complete job as `too_large`; a job
never silently truncates a post.

Video selection is compatibility-first. The extractors prefer H.264 video and
AAC audio in MP4, or an already compatible progressive MP4. Audio-only results
prefer M4A/AAC and then MP3. Accepted still-image formats remain JPEG, PNG,
WebP, and GIF. FFmpeg may merge or remux compatible streams with stream copy,
but it does not transcode codecs. When no compatible representation exists, the
job fails as `extraction_failed` instead of starting an expensive conversion.

## Architecture

The Phase 2 `worker.Extractor` interface remains unchanged:

```go
type Extractor interface {
    Extract(ctx context.Context, request extractor.Request) ([]extractor.File, error)
}
```

The method signature stays stable. `extractor.Request` adds `MaxItems int` so
every implementation receives the validated per-job item bound alongside the
existing URL, platform, work directory, and byte bound.

Production wiring replaces `extractor.Fake` with one composite real extractor.
The composite owns deterministic platform selection, safe tool execution,
output discovery, media inspection, and optional local remux. It returns only
final inspected files to the worker. The worker remains responsible for the job
timeout, database cancellation watch, free-space preflight, storage ingestion,
and lifecycle transitions.

The fake implementation remains available for unit tests and the executable
Bruno collection. A build tag named `omdi_testextractor` selects fake composition
for the collection-test binary. Normal development and release builds do not
include fake production wiring, and there is no runtime option that can
accidentally enable it.

### Package Boundaries

| Package | Phase 3 responsibility |
| --- | --- |
| `internal/urlpolicy` | Validate post-shaped URLs and provide the loopback egress proxy. This remains security-critical at 100% coverage. |
| `internal/extractor` | Composite extractor, platform routing, subprocess runner, `yt-dlp`, `gallery-dl`, `ffprobe`, and FFmpeg adapters. This becomes security-critical at 100% coverage. |
| `internal/config` | Add and validate `OMDI_MAX_JOB_ITEMS`; retain the existing absolute tool paths. |
| `internal/worker` | Pass the item limit to extraction and map typed extraction failures to existing public job errors. |
| `cmd/omdi` | Start the egress proxy, compose the real adapters, and provide build-tagged fake composition for collection tests. |
| `internal/storage` | Remain the final filesystem and media-type enforcement boundary without accepting paths outside the job work directory. |

No new package is introduced. Media inspection and processing remain in
`internal/extractor` because they exist only to validate and finish extractor
results.

## Direct-Post URL Policy

The API rejects URLs that do not identify one media post before creating a job.
After the existing lowercase-IDNA host normalization, Phase 3 accepts only these
path shapes, with one non-empty identifier in every placeholder:

| Platform | Accepted direct-post shapes |
| --- | --- |
| YouTube | `/watch?v={video}`, `/shorts/{video}`, `/live/{video}`, and `youtu.be/{video}` |
| Instagram | `/p/{shortcode}`, `/reel/{shortcode}`, `/reels/{shortcode}`, and `/tv/{shortcode}` |
| TikTok | `/@{handle}/video/{numeric-id}` and official `vm.tiktok.com`, `vt.tiktok.com`, or `/t/{token}` short links |
| X/Twitter | `/{handle}/status/{numeric-id}` and `/i/status/{numeric-id}` |
| Reddit | `/r/{subreddit}/comments/{post-id}`, `/comments/{post-id}`, `/gallery/{post-id}`, and `redd.it/{post-id}` |
| Vimeo | `/{numeric-id}` and `player.vimeo.com/video/{numeric-id}` |

An accepted path may have a trailing slash but no additional path that changes
its resource type. Profile, channel, playlist, search, feed, collection, and all
other routes are rejected. Identifier validation is ASCII and platform-specific;
empty identifiers, encoded separators, dot segments, and duplicate identity
parameters are rejected.

All non-identity query parameters are removed. YouTube watch URLs retain exactly
one `v` parameter; other accepted forms retain no query parameters. Fragments
remain discarded. Active live media is rejected during extraction even when its
URL shape is otherwise a valid direct-video route.

The normalized URL and platform ID continue to be stored on the job. URL
validation never performs an outbound request.

## Platform Routing

Routing is static and has no automatic fallback:

| Platforms | Adapter |
| --- | --- |
| YouTube, Vimeo, TikTok | `yt-dlp` |
| Instagram, X/Twitter, Reddit | `gallery-dl` |

This split gives video-oriented platforms to `yt-dlp` and post/carousel-oriented
platforms to `gallery-dl`. An adapter failure does not invoke the other tool,
which prevents duplicated downloads, inconsistent output, and unexpected
resource consumption.

## Egress Proxy

`internal/urlpolicy` provides one application-owned forward proxy bound to an
ephemeral IPv4 loopback address. It is never published by Compose. The worker's
single active extraction receives a job-scoped proxy session and budget; the
proxy rejects extractor traffic when no session is active.

The proxy supports ordinary HTTP forwarding and HTTPS `CONNECT` only. Targets
must use HTTP or HTTPS and port 80 or 443. Requests with user information,
malformed authorities, unsupported methods for tunneling, or other schemes and
ports are rejected.

Before every upstream connection, the proxy:

1. Resolves the hostname under a bounded context.
2. Requires every returned address to pass `urlpolicy.IsPublic`.
3. Connects with `urlpolicy.DialControl`, rechecking the selected address after
   resolution and defeating DNS rebinding.
4. Counts upstream-to-extractor bytes against the active job's budget.

The egress budget is `OMDI_MAX_JOB_BYTES` plus the greater of 1 MiB or five
percent of `OMDI_MAX_JOB_BYTES`. The margin covers manifests, protocol framing,
and metadata while leaving network use strictly bounded. Every byte received
again after a retry counts again. Exhausting the budget closes the upstream
connection and returns a typed `too large` failure.

Redirects are processed by the trusted extractor, but every redirected
connection must pass through the proxy and receives the same checks. Extractor
arguments explicitly set the proxy. The child environment omits `HTTP_PROXY`,
`HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, and lowercase equivalents, so host
configuration cannot replace or bypass the application proxy.

FFmpeg and `ffprobe` never receive remote URLs. Their protocol whitelist is
limited to local files.

## Subprocess Execution

A shared runner invokes only validated absolute executable paths from
configuration. It uses argument arrays and never a shell. Each command receives:

- the job context;
- the job work directory as its current directory;
- a private `HOME`, cache, and temporary directory inside that work directory;
- a small fixed environment rather than the parent environment;
- closed standard input;
- a new process group;
- bounded stdout and stderr capture.

Machine-readable stdout is limited to 256 KiB and diagnostic stderr to 64 KiB.
Overflow terminates the process as an invalid extraction result. Captured bytes
are used only for bounded parsing and error classification, then discarded.
They are never logged or stored.

Cancellation, timeout, output overflow, or egress exhaustion signals the entire
process group and allows a short grace period before sending a forced kill. This
also terminates FFmpeg or other descendants launched by a trusted extractor.
Compose sets explicit memory and process-count limits for the application
container. The existing job timeout and free-space check remain in force.

## `yt-dlp` Adapter

The adapter passes the submitted URL as one final argument and supplies fixed
options that:

- ignore all configuration and cache files;
- disable credentials, cookies, `.netrc`, playlists, live streams, subtitles,
  thumbnails, metadata sidecars, and generic URL extraction;
- use the application proxy and configured FFmpeg path;
- limit the operation to one download;
- write home and temporary outputs only inside the job work directory;
- use a server-controlled output template;
- prefer an iOS-compatible video/audio format and MP4 merge;
- emit only bounded machine-readable final-path information.

The adapter does not trust the printed path. After successful exit it enumerates
the work directory, selects the expected server-named final entry, and rejects
symlinks, subdirectories, auxiliary files, partial files, or multiple final
outputs. Any FFmpeg child used by `yt-dlp` operates on locally downloaded files.

## `gallery-dl` Adapter

The adapter ignores default configuration, uses the application proxy for both
extraction and downloads, stores its cache in memory, and writes only
server-named numbered entries in the work directory. Credentials, cookies,
archives, postprocessors, metadata sidecars, and recursive extraction are
disabled.

It requests at most `OMDI_MAX_JOB_ITEMS + 1` entries from the one submitted
post. Finding the extra entry proves that the post exceeds the configured
limit, so the complete job fails as `too_large`. The adapter never returns a
truncated carousel. Output order follows the source post's media order.

After a successful exit, the adapter enumerates the work directory rather than
trusting printed paths. Every expected numbered entry must be a top-level
non-symlink candidate. Missing, additional, nested, partial, or auxiliary
entries reject the job.

## Inspection and Remux

Each candidate passes through `ffprobe` before it is reported to the worker.
`ffprobe` runs non-interactively with error-only logging, JSON format and stream
output, and a local-file-only protocol whitelist. Its JSON output is size
limited and decoded with a closed internal schema for the fields the service
uses.

Inspection must find at least one supported media stream and must agree with the
actual container and codec combination. The extractor's extension and metadata
are hints only. Executable content, playlists, subtitle-only files, unknown
containers, unsupported codecs, and malformed probe output are rejected.

Already compatible files remain unchanged. A compatible H.264/AAC result that
only needs a different container is remuxed by the FFmpeg adapter with stream
copy, explicit stream maps, no standard input, no overwrite, error-only output,
and a local-file-only protocol whitelist. FFmpeg writes a fresh server-named
temporary output; the service validates it again with `ffprobe` before replacing
the candidate. Failure removes the temporary output and rejects the job. No
codec transcoding is attempted.

The inspected media type, not an extractor-supplied type, populates
`extractor.File.MediaType`. `storage.Ingest` remains the final boundary: it
moves only named top-level entries, rejects unsafe filesystem objects, enforces
the total output budget, changes permissions to `0600`, and assigns opaque item
IDs and download names.

## Failure Normalization and Privacy

Phase 3 does not add public job error codes:

| Condition | Public result |
| --- | --- |
| URL is not a supported direct-post route | API `422 unsupported_url`; no job is created |
| Item limit, egress budget, or output-byte limit is exceeded | Job `too_large` |
| Job context reaches its deadline | Job `timeout` |
| Media is private, removed, unavailable, live, unsupported, or tool output is invalid | Job `extraction_failed` |
| Unexpected local service, proxy, runner, or persistence failure | Job `internal` |

Context cancellation caused by an API cancellation, deletion, purge, or service
shutdown keeps the existing Phase 2 behavior and does not overwrite the winning
database state.

Logs contain only job ID, normalized platform, selected adapter, and normalized
error code. They never contain the submitted URL, redirected URL, headers,
cookies, proxy values, command line, tool diagnostics, filenames, internal
paths, media metadata, or remote response bodies. The normalized submitted URL
continues to be stored on its owner-scoped job and returned to that owner under
the existing API contract. Redirected URLs, headers, cookies, proxy values,
tool diagnostics, internal paths, extracted metadata, and remote response
bodies are neither stored in SQLite nor returned by the API.

## Configuration

Phase 3 adds one public setting:

| Variable | Default | Validation |
| --- | --- | --- |
| `OMDI_MAX_JOB_ITEMS` | `20` | Integer from 1 to 100. |

The proxy transfer budget is derived from `OMDI_MAX_JOB_BYTES` and is not a
second operator setting. Existing tool-path settings remain the source of the
four absolute executable paths. The application fails startup when construction
of the proxy or real extractor composition fails.

The Compose `app` service sets a 1 GiB memory limit and a 64-process PID limit.
These bounds allow the Go service, one Python extractor, and one FFmpeg child
while preventing unbounded process creation. Compose configuration rendering is
tested so neither limit can disappear unnoticed.

## HTTP and Collection Contracts

No endpoint, request shape, success representation, or download contract is
added. OpenAPI advances to `0.3.0` and clarifies that the submitted URL must
identify one public post, that carousels may yield multiple items, and how the
existing job error codes apply.

The Bruno requests keep their current organization and names. Required
collection tests build `omdi` with `omdi_testextractor`, so they exercise the
real API, queue, worker, storage, and download flow without external network
access. The release binary and normal `omdi serve` use the real composite.

## Opt-In Real Smoke Test

`make real-smoke URL=...` is an operator-invoked test and is not part of
`make ci`. It uses Docker Compose and the normal real-extractor build, creates a
temporary API key, submits exactly one job, polls it to a terminal state,
downloads every returned item, verifies that each file is non-empty, and then
removes the job, key, downloaded files, and Compose resources.

The target requires `URL`; it never supplies a default real-media URL. It does
not echo the URL, API key, download tokens, response bodies, or downloaded file
paths. Cleanup runs after success, failure, or interruption.

## Testing and Coverage

Behavior is developed with red-green-refactor TDD. Tests use local handlers,
controlled resolvers, and helper subprocesses; required tests never contact a
real platform.

### URL policy and egress

- Direct-post acceptance and profile, playlist, channel, collection, malformed,
  and tracking-parameter cases for every platform.
- HTTP forwarding and HTTPS `CONNECT` through loopback.
- Unsupported schemes, ports, authorities, credentials, and tunnel methods.
- Resolution failure, empty results, mixed public/private answers, special-use
  ranges, and connect-time rebinding.
- Redirect targets receiving independent validation.
- Egress accounting, retry accounting, limit exhaustion, context cancellation,
  inactive sessions, and concurrent-session rejection.
- Proxy output and errors remaining free of sensitive values.

### Subprocess runner and adapters

- Exact executable, argument array, fixed environment, current directory, and
  closed input for every tool.
- Config, cache, credential, playlist, live, recursive, generic-extractor, and
  network bypass defenses.
- Success, non-zero exit, launch failure, timeout, cancellation, grace-period
  kill, descendant kill, stdout overflow, stderr overflow, malformed structured
  output, and output redaction.
- Empty, missing, extra, nested, partial, symlink, hard-link, special-file,
  oversized, and too-many-item output.
- Stable carousel ordering and rejection rather than truncation.
- Valid and malformed `ffprobe` JSON, supported and unsupported stream sets,
  local-only inspection, successful stream-copy remux, remux failure, and
  post-remux reinspection.
- Static platform routing and proof that failures do not invoke a fallback.

### Worker, contracts, and Compose

- Typed extractor failures mapping to `too_large`, `timeout`,
  `extraction_failed`, and `internal` without changing cancellation races.
- API tests for direct-post validation and unchanged owner isolation.
- OpenAPI and Bruno assertions for the clarified contract.
- Fake collection-test build and real default build.
- Rendered Compose memory/PID limits and the opt-in smoke target's validation
  and cleanup behavior without running a real download in CI.

`internal/auth`, `internal/storage`, `internal/urlpolicy`, and
`internal/extractor` require 100% statement coverage. Total `internal/...`
coverage remains at least 90%. `make ci` is required before handoff when Docker
is available.

## Delivery Order

The implementation plan splits Phase 3 into the approved internal milestones
while keeping one phase-level specification:

1. Mark Phase 3 `In progress`; add item-limit configuration and direct-post URL
   policy.
2. Implement and compose the egress proxy.
3. Implement the bounded subprocess runner and `yt-dlp` adapter; switch normal
   production composition from fake to real.
4. Implement `ffprobe` inspection and FFmpeg stream-copy remux.
5. Implement `gallery-dl`, item-limit enforcement, and final static routing.
6. Normalize failures through the worker and synchronize OpenAPI and Bruno.
7. Add the build-tagged fake collection path, Compose resource limits, and
   opt-in real smoke target.
8. Update README, architecture, security documentation, roadmap status, and
   coverage gates; run `make ci`.

Phase 3 becomes `Complete` only after every milestone and verification step is
delivered. Phase 4 remains next.
