# Architecture

## Current milestone

The current application is the completed Phase 2 job lifecycle. It provides `omdi serve`, API-key and operator job-management commands, the validated configuration contract, SQLite persistence and queueing, owner-scoped authentication and repositories, URL normalization and destination-policy primitives, `GET /healthz`, dependency-backed `GET /readyz`, authenticated job creation, polling, and cancellation, and token-based media downloads. One bounded worker runs the fake extractor behind the `Extractor` boundary, ingests regular single-link files under server-generated names into private storage, and cooperates with startup recovery and periodic cleanup.

Phase 3 has not started. The application does not yet invoke `yt-dlp`, `gallery-dl`, `ffprobe`, or `ffmpeg`; every successful job still produces synthetic media through the fake extractor.

Implementation order, delivery status, and the next milestone are maintained in the [Roadmap](roadmap.md). Planned boundaries in this document do not override that order.

## Target runtime

The target self-hosted runtime will remain one Go binary containing the HTTP API and one bounded background worker. A SQLite-backed persistent queue will retain job state, media files will remain on disk, and short-lived opaque tokens will authorize downloads without exposing permanent API keys.

## Established and planned boundaries

The implemented packages separate configuration, HTTP contracts, application lifecycle, SQLite access, authentication, identifiers and job models, URL policy, readiness, private storage, job execution, extractor implementations, and cleanup. `cmd/omdi` remains the composition and CLI boundary. The worker consumes the demonstrated `Extractor` substitution boundary; Phase 2 supplies only the fake implementation.

Phase 3 will add real `yt-dlp` and `gallery-dl` adapters in `internal/extractor` and focused media inspection or processing adapters for `ffprobe` and `ffmpeg`. New interfaces or packages will be introduced only where those implementations demonstrate a real substitution boundary.

## Current data flow and planned extension

An authenticated client can submit a public media URL today. The API validates authentication, ownership, input, and URL normalization before persisting the job. The bounded worker claims it, runs the fake extractor in a private work directory, ingests the output, and records the resulting media item. Polling issues short-lived opaque download tokens, the download endpoint streams the stored file, and recovery and cleanup remove partial, orphaned, or expired state.

Phase 3 will keep this lifecycle while replacing fake extraction with platform selection, real network extraction, metadata inspection, and merge or remux processing. The planned iOS Shortcut remains a client of the same HTTP contract.

## Security invariants

Current URL handling accepts only supported HTTPS platform URLs and provides public-address resolution and connect-time checks. Phase 3 must wire those checks into extractor egress and enforce redirect policy for every real network path. Subprocesses will receive explicit argument arrays through context-aware execution; shell commands will never be built from user input. Extractor output, filenames, paths, metadata, and diagnostics will remain untrusted.

Authentication, resource ownership, constant-time secret handling, and indistinguishable cross-owner responses will be enforced. Logs and client errors will redact credentials, tokens, cookies, authorization headers, signed URLs, internal paths, and raw extractor output. Filesystem access will remain inside private server-controlled directories with restrictive permissions and protection against traversal and symlink escapes. Processing time, input length, output size, disk use, memory use, concurrency, and captured logs will be bounded.

## Explicit exclusions

The project will not provide DRM circumvention, private-media access, access-control bypasses, account-cookie management, public signup, a web administration panel, billing, Kubernetes deployment, external queues, object storage, multiple replicas, a centrally hosted service, or heavy transcoding as the default path.
