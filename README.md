# OpenMediaDownloaderIOS

OpenMediaDownloaderIOS is a self-hosted media download API intended for use from an iOS Shortcut. It provides authenticated endpoints to queue, poll, and cancel jobs, a persistent single-worker queue, real platform extractors, short-lived download links, and automatic cleanup.

## Requirements

- Docker with the Docker Compose plugin.
- Optional native development: Go 1.27.1.

No host installation of Go, Node.js, Bruno, or the media tools is required for the Compose workflow.

## Compose files

- `compose.yaml` is the production deployment: plain `docker compose` commands build the hardened runtime image and run it with a persistent data volume, in the Compose project `omdi`. See [Self-hosting](#self-hosting).
- `compose.development.yaml` is the development environment, in the project `omdi-dev`. The `Makefile` selects it for every target, so use `make` rather than plain `docker compose` while developing.

Both read the same `.env`, created from `.env.example`.

## Quick start

This starts the development environment:

```bash
cp .env.example .env
make compose-up
```

`make compose-up` stays attached and streams the service logs. Use `make compose-up-detached` to start the service in the background and return once it is healthy.

In another shell:

```bash
curl --fail http://localhost:8080/healthz
```

The response is:

```json
{"status":"ok"}
```

Readiness checks the database, the data directory and its free space, and the media tools:

```bash
curl --fail http://localhost:8080/readyz
```

It returns `{"status":"ready"}` or HTTP 503 with `{"status":"not_ready"}`, without identifying the failing check.

Stop the environment with:

```bash
make compose-down
```

The project-root `.env` configures both Compose files.

Compose publishes the API on `127.0.0.1:8080` only. To test from another device on a trusted local network, such as an iPhone, set `OMDI_PUBLISH_HOST=0.0.0.0` and point `OMDI_PUBLIC_URL` at an address that device can reach, for example `http://192.168.1.10:8080`, so returned `download_url` links work there. Traffic is plain HTTP, so API keys and download tokens cross the network unencrypted. Never publish this port to the internet.

## Self-hosting

To run your own server:

```bash
cp .env.example .env    # set OMDI_PUBLIC_URL to the address your devices reach
docker compose up --build --detach --wait
docker compose exec app omdi keys create --name phone
```

The service speaks plain HTTP and publishes on loopback by default; put a tunnel or a TLS reverse proxy in front of it before using it over the internet. [Self-Hosting](docs/self-hosting.md) covers disk sizing, operation, updates, backups, remote access, and hosts without persistent storage. [iOS Shortcut](docs/shortcut.md) explains how to build the Shortcut that uses the server.

## Configuration

Unset variables use their defaults. A variable that is set but empty, surrounded by whitespace, or invalid stops startup; errors name the variable but never echo its value.

| Variable | Default | Rule |
| --- | --- | --- |
| `OMDI_HTTP_ADDR` | `:8080` | `host:port`, port 1–65535. |
| `OMDI_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `OMDI_RESTORE_API_KEY_ON_STARTUP` | `false` | `true` or `false`; see [Startup key restoration](#startup-key-restoration). |
| `OMDI_API_KEY` | unset | Required when restoration is enabled: a key in the `omdi_<id>_<secret>` form. Ignored otherwise. |
| `OMDI_DATA_DIR` | `/data` | Absolute, clean path to a real directory; created with mode `0700` when missing. Holds `omdi.db` (mode `0600`). |
| `OMDI_ALLOWED_PLATFORMS` | `youtube,instagram,tiktok,x,reddit,vimeo` | Comma-separated subset of supported platforms. |
| `OMDI_MAX_URL_LENGTH` | `2048` | Integer 256–8192. |
| `OMDI_MAX_REQUEST_BYTES` | `16384` | Integer 1024–1048576. |
| `OMDI_JOB_TIMEOUT` | `10m` | Duration 30s–2h. |
| `OMDI_MAX_JOB_BYTES` | `2147483648` | Integer 1 MiB–100 GiB. |
| `OMDI_MIN_FREE_BYTES` | `1073741824` | Integer 0–1 TiB; readiness fails below it. A job starts only with this amount plus twice `OMDI_MAX_JOB_BYTES` free. |
| `OMDI_JOB_RETENTION` | `24h` | Duration 5m–720h, above `OMDI_JOB_TIMEOUT`. |
| `OMDI_TOKEN_TTL` | `15m` | Duration 1m–24h, not above `OMDI_JOB_RETENTION`. |
| `OMDI_YTDLP_PATH` | `/opt/media-tools/bin/yt-dlp` | Absolute, clean path. |
| `OMDI_GALLERYDL_PATH` | `/opt/media-tools/bin/gallery-dl` | Absolute, clean path. |
| `OMDI_FFMPEG_PATH` | `/opt/ffmpeg/bin/ffmpeg` | Absolute, clean path. |
| `OMDI_FFPROBE_PATH` | `/opt/ffmpeg/bin/ffprobe` | Absolute, clean path. |
| `OMDI_PUBLIC_URL` | `http://localhost:8080` | Absolute `http`/`https` URL without user info, query, or fragment; base of download links. |
| `OMDI_MAX_QUEUED_JOBS` | `10` | Integer 1–1000; queued plus running jobs allowed per API key. |
| `OMDI_MAX_JOB_ITEMS` | `20` | Integer 1–100; maximum media items per job. |


## API keys

API keys have the form `omdi_<id>_<secret>`. Only a SHA-256 hash of the secret is stored, and the full key is printed once on creation:

```bash
make key-create NAME=my-phone
make key-list
make key-revoke ID=<id>
make key-purge            # deletes every revoked key with all of its jobs and files
```

Revoked keys stay listed until `make key-purge` removes them. Names contain 1–64 letters, digits, `.`, `_`, or `-`. Clients send keys as `Authorization: Bearer <key>`; query-string keys are never accepted.

### Startup key restoration

Hosts whose data directory does not survive restarts lose every key along with the database. Setting `OMDI_RESTORE_API_KEY_ON_STARTUP=true` makes `omdi serve` insert `OMDI_API_KEY` before it starts listening whenever that key is missing, so clients keep working after data loss. Create the key once with `make key-create` and store it in the host's secret configuration.

- Only the SHA-256 hash of the secret is stored. A restored key is named `startup-<id>`.
- An existing record is never changed, and a record with the same ID but a different secret stops startup.
- A revocation lasts only as long as the database. To withdraw a restored key permanently, remove or replace `OMDI_API_KEY`.
- Restoration applies only to `omdi serve`; the `keys` and `jobs` commands ignore it.

## Jobs and downloads

```bash
KEY=omdi_...   # from make key-create
curl -s -X POST http://localhost:8080/v1/jobs \
  -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
  -d '{"url":"https://vimeo.com/7809605"}'
curl -s http://localhost:8080/v1/jobs/<id> -H "Authorization: Bearer $KEY"
```

- `POST /v1/jobs` queues a job (`202`). Each key may have `OMDI_MAX_QUEUED_JOBS` queued or running jobs.
- `GET /v1/jobs/{id}` returns the status. For a succeeded job, each item carries a `download_url` that expires after `OMDI_TOKEN_TTL`; every poll issues fresh links and invalidates earlier ones.
- `DELETE /v1/jobs/{id}` cancels a queued or running job.
- `GET /v1/downloads/{token}` streams the file without an API key and supports `Range`. `HEAD` returns the same headers without the body.

A failed job reports a fixed `error` code. For `extraction_failed`, `error_detail` gives the reason, derived from fixed patterns in the media tools' output and never from the output itself:

| `error_detail` | Meaning |
| --- | --- |
| `out_of_memory` | A media tool was killed by `SIGKILL`, usually by the kernel for lack of memory. |
| `login_required` | The platform requires a signed-in account. |
| `blocked` | The platform asked to confirm the client is not a bot, which is common from datacenter addresses. |
| `forbidden` | The platform refused a request with HTTP 403. |
| `rate_limited` | The platform limited the request rate. |
| `unavailable` | The post is private, removed, or does not exist. |
| `geo_restricted`, `age_restricted` | The platform restricts the post by region or age. |
| `no_media` | The post has no downloadable format. |
| `network_error` | A destination did not resolve or could not be reached. |
| `egress_denied` | The egress proxy refused a destination. |
| `invalid_output` | The downloaded files did not pass validation. |
| `processing_failed` | ffprobe or FFmpeg failed on the downloaded files. |
| `tool_error` | The tool failed without a known pattern. |

Failures detailed as `forbidden`, `rate_limited`, or `network_error` are retried once after five seconds, from an empty work directory and within the same `OMDI_JOB_TIMEOUT`.

Each job accepts exactly one public post URL; profiles, channels, playlists, feeds, and collections are rejected. YouTube, Vimeo, TikTok, Instagram, and Reddit route through `yt-dlp`; X routes through `gallery-dl`. Instagram links that start with the account handle, such as `/{handle}/p/{shortcode}/`, are accepted and stored without the handle. A carousel remains one job with up to `OMDI_MAX_JOB_ITEMS` ordered results (20 by default). Outputs are compatibility-first MP4/M4A, MP3, JPEG, PNG, WebP, GIF, or QuickTime media. FFmpeg is used only for local H.264/AAC stream-copy remuxing; incompatible codecs are rejected instead of transcoded.

Jobs, files, and tokens are removed automatically after `OMDI_JOB_RETENTION`. Operators can manage jobs from the CLI:

```bash
make job-list
make job-delete ID=<id>        # refuses running jobs
make job-purge [OWNER=<key-id>] # removes every job, including running ones
```

## Logs

The service writes one JSON object per line to standard error. Every line carries a `trace_id`: each HTTP request has one, returned in the `X-Trace-Id` response header, and so do each job attempt, each cleanup pass, and each CLI invocation. Request lines add the authenticated `key_id` and `key_name`, and job lines add `job_id` and `platform`, so a request that creates a job and the worker's run of it are joined by `job_id`.

- `info` logs one `request completed` line per request with the route pattern, status, size, and duration, plus job, download, authentication-rejection, cleanup, and startup events. Successful health checks are not logged.
- A `job failed` line includes `error_detail`, the failing `tool`, its `exit_code` or terminating `signal`, and the egress proxy's `egress_rejected` and `egress_failures` counts.
- `debug` adds up to five `extractor diagnostics` lines per failure: the tool's error lines with URLs, paths, and post IDs removed, non-ASCII characters replaced, and each line truncated to 240 characters. Enable it only while investigating failures.

Logs never include API keys, download tokens, submitted URLs, or internal paths; routes are logged by pattern, such as `/v1/downloads/{token}`.

## Bruno collection

Open `collection/` in Bruno and select the `local` environment for host-local requests. The collection contains the `health`, `jobs`, and `downloads` folders with executable assertions. Requests that need a key read it from `OMDI_API_KEY` in `collection/.env`.

For production, copy `collection/.env.example` to `collection/.env`, replace the placeholder with the real HTTPS base URL, and select `production`. This file is separate from the project-root `.env` and is ignored by Git.

Run the local collection against the Compose service without installing Bruno or Node.js. It creates a temporary API key and uses the build-tagged fake extractor so the required suite stays deterministic and offline. Production and default builds always use the real extractors.

```bash
make collection-test
```

## Development commands

| Command | Purpose |
| --- | --- |
| `make bootstrap` | Build the development image, download modules, and print tool versions. |
| `make fmt` | Format Go source through Compose. |
| `make fmt-check` | Verify Go formatting without modifying files. |
| `make test` | Run all Go tests with the race detector. |
| `make coverage` | Run unit tests and enforce at least 90% total coverage and 100% for security-critical packages. |
| `make lint` | Run `go vet` and GolangCI-Lint. |
| `make vuln` | Run `govulncheck`. |
| `make secret-scan` | Scan repository files for committed secrets. |
| `make build` | Build the `omdi` binary inside the tools container. |
| `make docker-build` | Build the non-root production image as `omdi:local`. |
| `make image-scan` | Scan the production image for fixed high or critical vulnerabilities. |
| `make smoke` | Validate `/healthz` inside the production image. |
| `make real-smoke URL=...` | Optionally exercise real extraction and network access, then purge temporary state. |
| `make compose-check` | Verify the development service's hardening and memory and PID limits. |
| `make production-smoke` | Start `compose.yaml` in an isolated project, check its hardening, health, readiness, and key management, then remove it. |
| `make compose-up` | Start the development service with Compose, attached to its logs. `make dev` is an alias. |
| `make compose-up-detached` | Start the development service in the background and wait until it is healthy. |
| `make collection-test` | Run the Bruno collection against a Compose service built with the fake extractor, then stop it. |
| `make compose-down` | Stop Compose services without deleting named data. |
| `make key-create NAME=...` | Create an API key in the Compose data volume and print it once. |
| `make key-list` | List API keys without secrets. |
| `make key-revoke ID=...` | Revoke an API key. |
| `make key-purge` | Delete every revoked API key with all of its jobs and files, including running jobs. |
| `make job-list` | List jobs without their source URLs. |
| `make job-delete ID=...` | Delete a job that is not running, with its files. |
| `make job-purge [OWNER=...]` | Remove every job, or one key's jobs, including running ones. |

## Selected versions

- Go 1.27.1
- Chi 5.3.2
- modernc.org/sqlite 1.59.0
- golang.org/x/net 0.59.0
- Python 3.14.7
- Bruno CLI 4.1.0
- GolangCI-Lint 2.13.2
- govulncheck 1.8.0
- yt-dlp 2026.08.19
- gallery-dl 1.32.13
- FFmpeg and ffprobe 9.0.2

Versions, container images, and downloaded artifacts are pinned. The project does not use floating tags or prerelease versions.

To update dependencies, verify the newest production-stable or supported LTS release from the official upstream source, update every related version, image digest, checksum, and documentation reference, regenerate `docker/media-tools.lock` from `docker/media-tools.in`, and run the complete `make ci` suite. Review lint, vulnerability, image-scan, and build results before accepting the update. Dependency updates are intentionally manual; Dependabot is not configured.

## Optional native workflow

With Go 1.27.1 installed, the current application can be exercised without Compose:

```bash
go test -race ./...
OMDI_DATA_DIR="$(mktemp -d)" go run ./cmd/omdi serve
```

The full supported workflow remains Compose-first because it supplies the pinned media tools and quality tooling.

## Current limitations and roadmap

Private media, authenticated sessions, DRM bypass, and transcoding are intentionally unsupported; a video that its platform marks as DRM-protected, as some Vimeo videos are, fails as `extraction_failed`. Because the service never sends cookies or credentials, platforms that require a login for anonymous access fail as `extraction_failed` with `error_detail` `login_required` or `blocked`; anti-bot checks are more frequent from datacenter addresses than from residential networks. When this was verified on 2026-09-27, every platform worked anonymously within these limits: Instagram photos, videos, and mixed carousels; X photos and videos; Reddit-hosted videos, while Reddit image and gallery posts fail; and Vimeo videos whose owners allow embedding, while embed-restricted videos fail. Results vary by post, network, and platform policy, and anonymous access can be rate limited. Phases 0–4 are complete; Phase 5, bulk ZIP downloads for multi-item jobs, is next. See the [Roadmap](docs/roadmap.md) and [Architecture](docs/architecture.md).

## Public repository safety

Never commit `.env` files, API keys, tokens, cookies, authorization headers, downloaded media, databases, logs, internal paths, or real production URLs. Use only synthetic examples and the tracked `.env.example` files.

## License

This project is available under the [MIT License](LICENSE).
