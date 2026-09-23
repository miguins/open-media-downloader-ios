# Architecture

## Current milestone

The current application is the completed Phase 1 secure core. It provides `omdi serve` and `omdi keys create|list|revoke`, the complete validated configuration contract, SQLite persistence with embedded migrations, hashed API keys with constant-time bearer authentication middleware, job/item/download-token domain models and owner-scoped repositories, URL normalization with a platform allowlist and public-address SSRF checks, unauthenticated `GET /healthz`, and `GET /readyz` backed by database, storage, and media-tool checks. Phase 2 has not started: no HTTP endpoint accepts jobs or requires authentication yet, and the application does not download media, run extractors, or issue download tokens.

Implementation order, delivery status, and the next milestone are maintained in the [Roadmap](roadmap.md). Planned boundaries in this document do not override that order.

## Target runtime

The target self-hosted runtime will remain one Go binary containing the HTTP API and one bounded background worker. A SQLite-backed persistent queue will retain job state, media files will remain on disk, and short-lived opaque tokens will authorize downloads without exposing permanent API keys.

## Planned boundaries

Future implementation will introduce focused packages for the API, authentication, database access, downloads, extractor adapters, media inspection and processing, security controls, and worker lifecycle. Extractor adapters will isolate `yt-dlp` and `gallery-dl`; media adapters will isolate `ffprobe` and `ffmpeg`. Interfaces will be introduced only at demonstrated substitution boundaries.

## Planned data flow

An iOS Shortcut will submit a public media URL and API key. The API will validate authentication, ownership, input, and URL policy before creating a persisted job. A bounded worker will claim the job, select an extractor, download into a private job directory, inspect or process the output, and record one or more media items. The API will issue temporary opaque download tokens, stream files to the Shortcut, and later remove expired tokens, jobs, partial files, and media.

## Security invariants

Future URL handling will accept only allowlisted public destinations and will defend against SSRF through strict scheme, hostname, address-range, resolution, and redirect checks. Subprocesses will receive explicit argument arrays through context-aware execution; shell commands will never be built from user input. Extractor output, filenames, paths, metadata, and diagnostics will remain untrusted.

Authentication, resource ownership, constant-time secret handling, and indistinguishable cross-owner responses will be enforced. Logs and client errors will redact credentials, tokens, cookies, authorization headers, signed URLs, internal paths, and raw extractor output. Filesystem access will remain inside private server-controlled directories with restrictive permissions and protection against traversal and symlink escapes. Processing time, input length, output size, disk use, memory use, concurrency, and captured logs will be bounded.

## Explicit exclusions

The project will not provide DRM circumvention, private-media access, access-control bypasses, account-cookie management, public signup, a web administration panel, billing, Kubernetes deployment, external queues, object storage, multiple replicas, a centrally hosted service, or heavy transcoding as the default path.
