# Architecture

## Current milestone

Phase 4 is implemented. The service combines the authenticated job API, SQLite queue, one bounded worker, short-lived downloads, recovery and cleanup with real platform extraction, and is packaged for self-hosting. Implementation order and delivery status remain authoritative in the [Roadmap](roadmap.md).

## Packaging

The `Dockerfile` builds a non-root `runtime` image with the pinned media tools. `compose.yaml` is the production deployment, project `omdi`: it runs that image with a persistent data volume, a read-only root filesystem, no capabilities, `no-new-privileges`, a 1 GiB memory limit, a 64-PID limit, and a loopback-published port meant to sit behind a tunnel or TLS reverse proxy. `compose.development.yaml`, project `omdi-dev`, compiles the source on start and adds the tools and Bruno services; the `Makefile` selects it. `make production-smoke` verifies the production file in CI.

## Runtime and boundaries

One Go binary owns the HTTP server and background work. `cmd/omdi` composes configuration, storage, URL policy, the real extractor, worker, cleaner, and readiness checks. Package boundaries remain focused: `internal/api` owns HTTP contracts, `internal/logging` trace IDs and scoped log attributes, `internal/store` persistence, `internal/storage` private file ingestion, `internal/urlpolicy` normalization and egress policy, `internal/extractor` tools and routing, and `internal/worker` job execution.

Production uses `extractor.Real`. YouTube, Vimeo, TikTok, Instagram, and Reddit route statically to the `yt-dlp` adapter; X routes to `gallery-dl`. Each platform uses the tool that extracts it without credentials. Instagram runs `yt-dlp` in a post-media mode that also delivers photo entries through their thumbnails. The adapter hands Vimeo and Reddit URLs to `yt-dlp` in their logged-out forms, the Vimeo embed player and the Reddit comments route, without changing the stored job URL. The Bruno collection builds the existing fake extractor only with the private `omdi_testextractor` build tag, keeping required executable contracts offline without adding a runtime switch.

## Data flow

The API authenticates the owner, canonicalizes exactly one direct public-post URL, and persists an independent job. The worker claims it, creates a private work directory, and gives the real extractor the job byte and item limits. The extractor opens one loopback egress-proxy session, invokes one fixed adapter through the bounded subprocess runner, and enumerates only top-level regular single-link output files.

ffprobe classifies local content from a closed metadata schema; extensions are never trusted. Approved iOS-compatible media passes through. H.264 with optional AAC in a compatible non-MP4 container may be remuxed locally by FFmpeg using stream copy, reinspected, and transactionally installed. No heavy transcoding occurs. Storage ingests validated files under server-generated names, and polling issues short-lived opaque download tokens.

A failed extraction becomes a `Failure` with a fixed `error_detail`: a SIGKILL the runner did not send means the kernel killed the tool, known error-line patterns name platform refusals, media-tool and validation failures have their own details, and the egress session's rejection and failure counts explain otherwise unrecognized failures. The worker stores the detail, retries transient details once, and logs the tool's exit status. Every log line carries a trace ID from a context-scoped `slog` handler; requests, job attempts, cleanup passes, and CLI invocations each start a trace.

## Security invariants

Every extractor connection is resolved and checked for public addressing at connection time. The job-scoped proxy limits schemes, ports, redirects, response headers, and aggregate transferred bytes, including CONNECT tunnels. Subprocesses receive explicit arguments, isolated environments, bounded diagnostics, context cancellation, and process-group cleanup.

URLs, remote data, filenames, paths, metadata, and diagnostics are attacker-controlled. Work remains confined to private server directories; symlinks, hard links, traversal, nested output, sidecars, incompatible codecs, excess items, and excess bytes fail closed. Authentication, owner isolation, restrictive file permissions, disk reservations, timeouts, one-worker concurrency, the 1 GiB memory limit, and the 64-PID limit remain enforced. Logs and client errors do not expose secrets, URLs, internal paths, or raw diagnostics; only the `debug` level adds bounded, sanitized tool error lines.

## Explicit exclusions

The project does not provide DRM circumvention, private-media access, account cookies, credential forwarding, public signup, a web administration panel, billing, Kubernetes deployment, external queues, object storage, multiple replicas, a centrally hosted service, or transcoding as a fallback.
