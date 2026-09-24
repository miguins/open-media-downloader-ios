# Phase 3 Real Extractors Delivery Record

## Delivered scope

Phase 3 implements canonical direct-post URL policy, job-scoped validating egress, bounded subprocess execution, local media inspection, H.264/AAC stream-copy remuxing, real `yt-dlp` and `gallery-dl` adapters, static platform routing, typed worker errors, bounded carousel results, build-selected offline collection extraction, and an opt-in real-network smoke workflow.

The approved inputs were the [design specification](../specs/2026-09-23-phase-3-real-extractors-design.md) and [implementation plan](../plans/2026-09-23-phase-3-real-extractors.md).

## Verification evidence

- `make fmt` and `make fmt-check`: passed.
- `make test`: race-enabled suite passed.
- `make coverage`: passed at 96.7% total; `internal/auth`, `internal/storage`, `internal/urlpolicy`, and `internal/extractor` each reached 100%.
- `make lint`: `go vet` and GolangCI-Lint passed with zero findings.
- `make vuln`: no known Go vulnerabilities found.
- `make secret-scan`: passed with no committed-secret findings.
- `make build`, `make docker-build`, `make smoke`, and `make image-scan`: passed.
- `make compose-check`: confirmed the 1 GiB memory and 64 PID limits.
- `make collection-test`: passed with the fake-tagged offline extractor.
- `sh scripts/test-real-smoke.sh`: the offline smoke harness passed, including failure cleanup.
- `make ci`: passed after the complete aggregate gate.

## Security invariants

Extractor traffic is confined to a per-job loopback proxy that revalidates public destinations and accounts for one aggregate transfer budget. Tools run without a shell or ambient credentials, in isolated directories, with bounded diagnostics and process-group cancellation. Local output is treated as hostile, inspected by content, confined to the work directory, and rejected on traversal, links, sidecars, incompatible codecs, excess bytes, or excess items. Errors and logs contain neither submitted URLs nor raw diagnostics.

## Post-delivery corrections

A review on 2026-09-24 ran the pinned tools against real media and found defects that the fake-tool unit tests could not reveal. Every real job failed until these were corrected:

- `ffprobe` rejected `-nostdin`, an option that only `ffmpeg` accepts, so every inspection failed. The runner already closes standard input.
- `yt-dlp --max-downloads 1` exits with status 101 after a successful download. The option was removed; the URL policy and `--no-playlist` already bound a job to one post.
- The `yt-dlp` format selector matched only `avc1`/`mp4a` codec names, while the TikTok extractor reports `h264`/`aac`. The selector now matches both spellings and no longer falls back to video without audio.
- `gallery-dl` numbers single-media Reddit posts `0`, which discovery rejected as a gap. A sole `item-000` entry is now accepted.
- Oversized files skipped by `yt-dlp` or `gallery-dl` exit successfully. The adapters now detect the tools' size markers and fail the job as `too_large` instead of `extraction_failed`.
- `gallery-dl` followed external links, such as Reddit link posts, through child extractors. An empty extractor whitelist now disables them.
- The worker's free-space preflight now reserves twice `OMDI_MAX_JOB_BYTES`, because merging and remuxing keep an input and its output on disk at the same time.
- `make real-smoke` joined the absolute `download_url` to its base URL and could never succeed. It now downloads the URL's path from the local listener. The offline harness now runs in `make ci`.

Real-network verification on 2026-09-24: `make real-smoke` passed for a public YouTube video, and a public TikTok video completed as one `video/mp4` item. Instagram redirected anonymous access to its login page. X returned no guest results. Reddit blocked the test network. The tested Vimeo video required a logged-in web client. These are platform access policies. Supporting them would require the credential support that Phase 3 excludes.

## Deferred work

The iOS Shortcut package and remaining Phase 4 packaging and documentation work remain deferred.
