# OpenMediaDownloaderIOS

OpenMediaDownloaderIOS is the foundation of a self-hosted media download API intended for use from an iOS Shortcut. The current milestone provides the secure core: validated configuration, SQLite persistence, API-key management, authentication middleware, URL and SSRF policy, and unauthenticated `GET /healthz` and `GET /readyz`. Job endpoints, media downloads, and extractors are roadmap work.

## Requirements

- Docker with the Docker Compose plugin.
- Optional native development: Go 1.27.1.

No host installation of Go, Node.js, Bruno, or the media tools is required for the Compose workflow.

## Quick start

```bash
cp .env.example .env
make compose-up
```

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

The project-root `.env` configures local Compose development.

## Configuration

Unset variables use their defaults. A variable that is set but empty, surrounded by whitespace, or invalid stops startup; errors name the variable but never echo its value.

| Variable | Default | Rule |
| --- | --- | --- |
| `OMDI_HTTP_ADDR` | `:8080` | `host:port`, port 1–65535. |
| `OMDI_DATA_DIR` | `/data` | Absolute, clean path to a real directory; created with mode `0700` when missing. Holds `omdi.db` (mode `0600`). |
| `OMDI_ALLOWED_PLATFORMS` | `youtube,instagram,tiktok,x,reddit,vimeo` | Comma-separated subset of supported platforms. |
| `OMDI_MAX_URL_LENGTH` | `2048` | Integer 256–8192. |
| `OMDI_MAX_REQUEST_BYTES` | `16384` | Integer 1024–1048576. |
| `OMDI_JOB_TIMEOUT` | `10m` | Duration 30s–2h. |
| `OMDI_MAX_JOB_BYTES` | `2147483648` | Integer 1 MiB–100 GiB. |
| `OMDI_MIN_FREE_BYTES` | `1073741824` | Integer 0–1 TiB; readiness fails below it. |
| `OMDI_JOB_RETENTION` | `24h` | Duration 5m–720h. |
| `OMDI_TOKEN_TTL` | `15m` | Duration 1m–24h, not above `OMDI_JOB_RETENTION`. |
| `OMDI_YTDLP_PATH` | `/opt/media-tools/bin/yt-dlp` | Absolute, clean path. |
| `OMDI_GALLERYDL_PATH` | `/opt/media-tools/bin/gallery-dl` | Absolute, clean path. |
| `OMDI_FFMPEG_PATH` | `/opt/ffmpeg/bin/ffmpeg` | Absolute, clean path. |
| `OMDI_FFPROBE_PATH` | `/opt/ffmpeg/bin/ffprobe` | Absolute, clean path. |

Job, size, retention, and token settings are validated now and enforced by the job lifecycle in Phase 2.

## API keys

API keys have the form `omdi_<id>_<secret>`. Only a SHA-256 hash of the secret is stored, and the full key is printed once on creation:

```bash
make key-create NAME=my-phone
make key-list
make key-revoke ID=<id>
```

Names contain 1–64 letters, digits, `.`, `_`, or `-`. Clients will send keys as `Authorization: Bearer <key>`; query-string keys are never accepted. No endpoint requires a key until Phase 2 adds job endpoints.

## Bruno collection

Open `collection/` in Bruno and select the `local` environment for host-local requests. The collection contains the organized `health/healthz` and `health/readyz` requests and executable assertions.

For production, copy `collection/.env.example` to `collection/.env`, replace the placeholder with the real HTTPS base URL, and select `production`. This file is separate from the project-root `.env` and is ignored by Git.

Run the local collection against the Compose service without installing Bruno or Node.js:

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
| `make compose-up` | Start the development service with Compose. |
| `make collection-test` | Run the Bruno collection against the Compose service. |
| `make compose-down` | Stop Compose services without deleting named data. |
| `make key-create NAME=...` | Create an API key in the Compose data volume and print it once. |
| `make key-list` | List API keys without secrets. |
| `make key-revoke ID=...` | Revoke an API key. |

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

There are no job endpoints, queue, worker, download endpoint, extractor invocation, or iOS Shortcut package yet.

Phases 0 (project foundation) and 1 (secure core) are complete. The next development effort is Phase 2, the job lifecycle. See the ordered delivery status in the [Roadmap](docs/roadmap.md) and the planned system boundaries in [Architecture](docs/architecture.md).

## Public repository safety

Never commit `.env` files, API keys, tokens, cookies, authorization headers, downloaded media, databases, logs, internal paths, or real production URLs. Use only synthetic examples and the tracked `.env.example` files.

## License

This project is available under the [MIT License](LICENSE).
