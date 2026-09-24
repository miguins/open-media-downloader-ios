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

## Deferred work

The optional real-network smoke was not run against public media during required verification. The iOS Shortcut package and remaining Phase 4 packaging and documentation work remain deferred.
