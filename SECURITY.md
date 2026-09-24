# Security Policy

## Supported versions

The unreleased `main` branch is currently the only supported version. No published release exists yet.

## Reporting a vulnerability

Use GitHub private vulnerability reporting for this repository under **Security → Advisories → Report a vulnerability**. Do not open a public issue or discussion containing exploit details.

Include the security impact, affected revision, minimal reproduction steps, and a suggested mitigation when available. Use synthetic data only; never include real secrets, tokens, personal data, private URLs, or downloaded media.

## Extractor security boundary

The pinned `yt-dlp`, `gallery-dl`, FFmpeg, and ffprobe binaries are trusted dependencies. Submitted URLs, remote responses, generated filenames, local extractor output, metadata, and diagnostics are hostile input. Extractors receive fixed argument arrays without a shell, isolated cache directories, bounded output capture, process-group cancellation, and no cookies, credentials, browser state, netrc, or arbitrary request headers.

All extractor network access uses a job-scoped loopback proxy. It permits only HTTP and HTTPS on ports 80 and 443, resolves and revalidates every connection, rejects non-public destinations and DNS rebinding, and applies a single byte budget across responses and tunnels. Local ffprobe and FFmpeg invocations allow only file input; FFmpeg performs stream-copy remuxing and never fetches network resources.

Jobs are bounded by URL, request, byte, item, time, disk, diagnostic, memory, and PID limits. Output ingestion rejects traversal, symlinks, hard links, non-regular files, unknown sidecars, and unsupported media. Errors and logs never include API keys, tokens, cookies, authorization headers, source or signed URLs, internal paths, or raw tool diagnostics. Private-media access, DRM circumvention, account-cookie support, and credential forwarding are out of scope.
