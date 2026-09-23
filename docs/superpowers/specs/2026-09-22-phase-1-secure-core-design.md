# Phase 1 — Secure Core Design

## Context

Phase 0 delivered `omdi serve`, `GET /healthz`, a Compose-first workflow, and the quality gates described in [the Phase 0 design](2026-09-20-initial-project-foundation-design.md). Phase 1 builds the secure core that Phase 2 job handling depends on: complete configuration, SQLite persistence, API keys and authentication, job/item/token domain models, public-media URL policy with SSRF defenses, and a readiness endpoint backed by real checks.

Phase 1 still does not download media, run extractors, accept jobs over HTTP, issue download tokens, or stream files.

## Goals

- Load and validate every setting the runtime needs, failing fast with messages that never echo secret or attacker-controlled values.
- Persist API keys, jobs, items, and download tokens in SQLite through versioned, embedded migrations.
- Let an operator create, list, and revoke API keys from the `omdi` CLI; permanent keys are shown only once and stored only as hashes.
- Provide authentication middleware with constant-time verification and uniform failure responses.
- Model job, item, and download-token state with explicit, tested state transitions.
- Normalize submitted URLs, enforce a platform allowlist, and reject non-public network destinations.
- Expose unauthenticated `GET /readyz` that reports readiness from real dependency checks without disclosing details.

## Non-Goals

- Job endpoints, the worker, the queue loop, and the fake extractor (Phase 2).
- Download-token issuance and the streaming endpoint (Phase 2).
- Invoking `yt-dlp`, `gallery-dl`, `ffmpeg`, or `ffprobe` beyond readiness checks that they are executable (Phase 3).
- Rate limiting, multi-user signup, or an admin HTTP API.

## Package Boundaries

| Package | Responsibility |
| --- | --- |
| `cmd/omdi` | Parse commands (`serve`, `keys create|list|revoke`), compose dependencies, map errors to exit codes. |
| `internal/config` | Load and validate all environment configuration. |
| `internal/store` | Open SQLite, apply embedded migrations, and implement repositories. |
| `internal/auth` | API-key generation, parsing, hashing, verification, and HTTP middleware. |
| `internal/id` | Random 128-bit record identifiers shared by keys, jobs, and items. |
| `internal/job` | Job, item, and download-token domain types, statuses, and transitions. |
| `internal/urlpolicy` | URL normalization, platform allowlist, public-address classification, and resolution and dial-time checks. |
| `internal/readiness` | Dependency checks aggregated into one ready/not-ready result. |
| `internal/api` | Router, `/healthz`, `/readyz`. |
| `internal/app` | Server lifecycle (unchanged except for receiving the configured handler). |

No other packages are created. Interfaces exist only where a consumer needs substitution in tests or across packages: the readiness checker list and the repository methods consumed by `auth`.

## Configuration Contract

All variables use the `OMDI_` prefix. Unset variables use the default; a variable that is present but empty, surrounded by whitespace, or invalid fails startup. Errors name the variable and the rule, never the supplied value.

| Variable | Default | Rule |
| --- | --- | --- |
| `OMDI_HTTP_ADDR` | `:8080` | `host:port`, port 1–65535 (unchanged). |
| `OMDI_DATA_DIR` | `/data` | Absolute, clean path; must not be a symlink. Created with mode `0700` when missing. Holds `omdi.db`; Phase 2 adds job directories. |
| `OMDI_ALLOWED_PLATFORMS` | all supported | Comma-separated subset of built-in platform identifiers. Unknown identifiers fail. |
| `OMDI_MAX_URL_LENGTH` | `2048` | Integer 256–8192. |
| `OMDI_MAX_REQUEST_BYTES` | `16384` | Integer 1024–1048576. |
| `OMDI_JOB_TIMEOUT` | `10m` | Go duration 30s–2h. |
| `OMDI_MAX_JOB_BYTES` | `2147483648` | Integer bytes, 1 MiB–100 GiB. |
| `OMDI_MIN_FREE_BYTES` | `1073741824` | Integer bytes, 0–1 TiB. Readiness fails below it. |
| `OMDI_JOB_RETENTION` | `24h` | Go duration 5m–720h. |
| `OMDI_TOKEN_TTL` | `15m` | Go duration 1m–24h; must not exceed `OMDI_JOB_RETENTION`. |
| `OMDI_YTDLP_PATH` | `/opt/media-tools/bin/yt-dlp` | Absolute, clean path. |
| `OMDI_GALLERYDL_PATH` | `/opt/media-tools/bin/gallery-dl` | Absolute, clean path. |
| `OMDI_FFMPEG_PATH` | `/opt/ffmpeg/bin/ffmpeg` | Absolute, clean path. |
| `OMDI_FFPROBE_PATH` | `/opt/ffmpeg/bin/ffprobe` | Absolute, clean path. |

Configuration validation collects all errors and reports them together. Limits that Phase 1 does not enforce yet (`OMDI_JOB_TIMEOUT`, `OMDI_MAX_JOB_BYTES`, `OMDI_JOB_RETENTION`, `OMDI_TOKEN_TTL`) are validated now so the contract is stable before Phase 2 consumes them. `.env.example`, `compose.yaml`, and the README document every variable.

## Persistence

- Driver: `modernc.org/sqlite` (pure Go; keeps `CGO_ENABLED=0` builds), pinned to the newest stable release at implementation time.
- Connection pragmas: `foreign_keys=ON`, `journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`. One writer connection pool limit.
- Database file and its WAL/SHM siblings are created with mode `0600` inside the `0700` data directory; startup refuses a database path that is a symlink.
- Migrations are embedded `.sql` files applied in order within a transaction and tracked through `PRAGMA user_version`. The runner is in-house (a few dozen lines) rather than an added dependency. A database newer than the binary fails startup.
- `serve` and every `keys` subcommand open the store and apply pending migrations.

### Schema v1

```text
api_keys(id TEXT PK, name TEXT NOT NULL UNIQUE, secret_hash BLOB NOT NULL,
         created_at INTEGER NOT NULL, last_used_at INTEGER, revoked_at INTEGER)

jobs(id TEXT PK, owner_id TEXT NOT NULL REFERENCES api_keys(id),
     source_url TEXT NOT NULL, platform TEXT NOT NULL,
     status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','canceled')),
     error_code TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
     started_at INTEGER, finished_at INTEGER, expires_at INTEGER NOT NULL)

items(id TEXT PK, job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
      position INTEGER NOT NULL, file_name TEXT NOT NULL, media_type TEXT NOT NULL,
      size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0), created_at INTEGER NOT NULL,
      UNIQUE (job_id, position))

download_tokens(token_hash BLOB PK, item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
                owner_id TEXT NOT NULL REFERENCES api_keys(id),
                created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL)
```

Timestamps are Unix milliseconds in UTC. IDs are 128-bit random values encoded as lowercase base32 without padding. Items store only a server-generated file name; the on-disk location is derived from the data directory and job ID, never stored as a free-form path. Jobs and items are always queried with `owner_id`, so cross-owner access returns the same "not found" as a missing record. Download tokens are looked up by hash, because the token itself is the download credential, and carry `owner_id` for auditing and cleanup.

Phase 1 repositories cover: API-key create/get/list/revoke/touch; job create/get-by-owner/transition; item create/list-by-job; token create/get-valid/delete-expired. Phase 2 adds only what its flows require.

## API Keys and Authentication

- Format: `omdi_<id>_<secret>`, where `<id>` is the record ID and `<secret>` is 32 random bytes encoded as unpadded base64url. The fixed prefix makes keys recognizable to secret scanners.
- Storage: SHA-256 of the secret. The secret is high-entropy, so a slow password hash adds no protection.
- Verification: parse strictly; look up by ID; compare hashes with `crypto/subtle.ConstantTimeCompare`. Unknown or revoked IDs still perform a comparison against a fixed dummy hash. All failures produce the same `401` with `WWW-Authenticate: Bearer` and body `{"error":"unauthorized"}`.
- Transport: `Authorization: Bearer <key>` only. Keys in query strings are not accepted.
- Successful verification stores the key ID in the request context and updates `last_used_at` at most once per minute per key.
- The middleware is implemented and fully tested in Phase 1 and mounted on the first protected routes in Phase 2; no placeholder protected route is added.

CLI:

```text
omdi keys create --name <name>   # prints the full key once on stdout
omdi keys list                   # id, name, created, last used, revoked; never secrets
omdi keys revoke <id>
```

Names are 1–64 characters of `[A-Za-z0-9._-]`. `make key-create NAME=...`, `make key-list`, and `make key-revoke ID=...` run the commands through Compose against the application volume, passing arguments through the environment rather than shell interpolation.

## Domain Models

`internal/job` defines `Job`, `Item`, `DownloadToken`, and `Status`. Allowed transitions:

```text
queued  -> running | canceled
running -> succeeded | failed | canceled
```

Terminal statuses do not transition. `Transition` returns an error for anything else and sets `started_at`/`finished_at`/`updated_at`. Error codes are a closed enumeration (`unsupported_url`, `extraction_failed`, `too_large`, `timeout`, `internal`) so raw extractor diagnostics never reach clients.

## URL Policy and SSRF Defenses

`urlpolicy.Normalize(raw)` accepts a URL only when all rules pass:

1. Length at most `OMDI_MAX_URL_LENGTH`; valid UTF-8; no control characters or surrounding whitespace.
2. Scheme `https` (an `http` input is rejected, not upgraded).
3. No user info; port absent or `443`.
4. Host converted with IDNA to ASCII, lowercased, trailing dot removed; IP literals rejected.
5. Host equals, or is a subdomain of, a domain belonging to an allowed platform.
6. Fragment removed; path and query otherwise preserved.

It returns the normalized URL and the platform identifier. Built-in platforms and domains:

| Platform | Domains |
| --- | --- |
| `youtube` | `youtube.com`, `youtu.be` |
| `instagram` | `instagram.com` |
| `tiktok` | `tiktok.com` |
| `x` | `x.com`, `twitter.com` |
| `reddit` | `reddit.com`, `redd.it` |
| `vimeo` | `vimeo.com` |

`urlpolicy.IsPublic(netip.Addr)` rejects unspecified, loopback, private, link-local, CGNAT (`100.64.0.0/10`), multicast, broadcast, documentation, benchmarking, reserved, unique-local, NAT64, 6to4, Teredo, and IPv4-mapped forms of any rejected address.

`urlpolicy.CheckResolved(ctx, resolver, host)` resolves the host with a bounded timeout and fails if resolution fails, returns nothing, or returns any non-public address. `urlpolicy.DialControl` is a `net.Dialer.Control` function that rejects non-public addresses at connect time, defeating DNS rebinding for any Go HTTP client the service uses. Extractor egress control is designed in Phase 3.

All rejections map to a single client-facing reason (`unsupported_url`); the specific rule is logged at debug level without the URL.

## Readiness

`GET /readyz` is unauthenticated. It runs each check with a shared 2-second timeout:

- Database: `PingContext` plus reading `user_version` equal to the expected schema version.
- Storage: data directory exists, is not a symlink, is writable (create and remove a temporary file), and free space is at least `OMDI_MIN_FREE_BYTES`.
- Media tools: each configured path is a regular executable file.

Responses:

- `200` with `{"status":"ready"}\n` when every check passes.
- `503` with `{"status":"not_ready"}\n` otherwise. The failing check name is logged; paths, errors from the OS, and tool output are not included in the response.

The Compose and image health checks stay on `/healthz`. `/readyz` is added to `docs/openapi.yaml` and to the Bruno collection as `health/readyz.yml` asserting status `200` and `res.body.status` equal to `ready`.

## Logging and Errors

- `slog` JSON logging remains. No code path logs request headers, keys, submitted URLs, or paths; tests assert this for authentication, readiness, and the CLI. A shared redaction helper is introduced together with the first request logging rather than ahead of it.
- HTTP error bodies are fixed JSON objects with a stable `error` code.
- Startup errors identify the failing component, not paths or values.

## Testing and Coverage

- TDD for every behavior. Unit tests beside packages; the SQLite store is tested against temporary on-disk databases.
- Table-driven tests cover every configuration rule, migration ordering and newer-schema refusal, key parsing edge cases, constant-time path selection, every URL rule, every special-purpose IPv4/IPv6 range, rebinding via `DialControl`, and each readiness failure.
- `scripts/check-coverage.sh` gains a per-package gate: `internal/auth` and `internal/urlpolicy` require 100%; total stays at least 90%.
- `make ci` remains the handoff gate and runs the Bruno collection, now including `readyz`.

## Documentation Updates

- `docs/roadmap.md`: Phase 1 `In progress` when implementation starts; `Complete` at handoff.
- `docs/architecture.md`: current-milestone section describes the delivered secure core.
- `README.md`, `.env.example`, `compose.yaml`: configuration table and key-management commands.
- `AGENTS.md`: project map and package boundaries.

## Delivery Order

1. Configuration contract.
2. Store and migrations (schema v1).
3. Domain models.
4. API keys: generation, repository, CLI.
5. Authentication middleware.
6. URL policy and SSRF defenses.
7. Readiness checks, `/readyz`, OpenAPI, and Bruno.
8. Documentation, coverage gate, and `make ci`.

Each step is an atomic conventional commit when commits are authorized.
