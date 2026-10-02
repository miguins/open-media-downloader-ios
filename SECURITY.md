# Security Policy

## Supported versions

Security fixes are made on `main` and published in the next release. Only the latest release is supported; update to it before reporting an issue.

Every published image carries a signed build provenance attestation. Verify an image before deploying it with `gh attestation verify oci://ghcr.io/miguins/open-media-downloader-ios:<version> --repo miguins/open-media-downloader-ios`.

## Reporting a vulnerability

Use GitHub private vulnerability reporting for this repository under **Security → Advisories → Report a vulnerability**. Do not open a public issue or discussion containing exploit details.

Include the security impact, affected revision, minimal reproduction steps, and a suggested mitigation when available. Use synthetic data only; never include real secrets, tokens, personal data, private URLs, or downloaded media.

## Extractor security boundary

The pinned `yt-dlp`, `gallery-dl`, FFmpeg, and ffprobe binaries are trusted dependencies. Submitted URLs, remote responses, generated filenames, local extractor output, metadata, and diagnostics are hostile input. Extractors receive fixed argument arrays without a shell, isolated cache directories, bounded output capture, process-group cancellation, and no cookies, credentials, browser state, netrc, or arbitrary request headers.

All extractor network access uses a job-scoped loopback proxy. It permits only HTTP and HTTPS on ports 80 and 443, resolves and revalidates every connection, rejects non-public destinations and DNS rebinding, and applies a single byte budget across responses and tunnels. Local ffprobe and FFmpeg invocations allow only file input; FFmpeg performs stream-copy remuxing and never fetches network resources.

Jobs are bounded by URL, request, byte, item, time, disk, diagnostic, memory, and PID limits. Output ingestion rejects traversal, symlinks, hard links, non-regular files, unknown sidecars, and unsupported media. Errors and logs never include API keys, tokens, cookies, authorization headers, source or signed URLs, internal paths, or raw tool diagnostics. Failure reasons are fixed codes derived from tool output, not copies of it. Only at the `debug` log level, a failure also logs up to five of the tool's error lines, bounded and sanitized: URLs, absolute paths, and post IDs are removed and non-ASCII characters replaced. Treat debug logs as sensitive.

When `OMDI_RESTORE_API_KEY_ON_STARTUP` is enabled, `OMDI_API_KEY` holds a complete API key; keep it in the host's secret store. Only its hash reaches the database, and it never appears in logs. Private-media access, DRM circumvention, account-cookie support, and credential forwarding are out of scope.

## Bundle downloads

A bundle combines the validated media of one job in ZIP format. Entries use
server-generated plain names, preserve item order, and use no compression.
Construction is bounded by item/media budgets plus fixed ZIP overhead, checks
source sizes and single-link regular files, honors cancellation, and installs
only a complete private file. Startup recovery removes unfinished-job artifacts.

`/v1/bundles/{token}` accepts an opaque temporary credential distinct from item
tokens; a job ID never grants unauthenticated access. Treat both link types as
secrets. Polling replaces previous tokens and expiry cannot outlive the job.
API-key revocation prevents further polling; issued download links remain usable
until expiry or job/key purge. Remove the jobs or purge a revoked key when issued
links must be withdrawn immediately. Authentication errors and downloads,
including unsatisfiable-range errors, send `Cache-Control: no-store`.

## Deployment

- The service speaks plain HTTP. Outside a trusted local network, serve it only through a tunnel or a TLS reverse proxy, and keep the container port published on loopback, the `compose.yaml` default.
- API keys grant job creation and media access. Store them in a password manager or the host's secret store, never in shared Shortcut copies, and revoke a key as soon as it may have leaked.
- Treat `debug` logs as sensitive and keep the default `info` level in normal operation.
- The container runs as a non-root user with a read-only root filesystem, no capabilities, `no-new-privileges`, and memory and PID limits; keep those settings when adapting `compose.yaml`.
- Update regularly. Pinned media tools receive fixes for parsing and security issues only through repository updates, followed by a rebuild.
