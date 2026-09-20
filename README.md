# OpenMediaDownloaderIOS

OpenMediaDownloaderIOS is the foundation of a self-hosted media download API intended for use from an iOS Shortcut. The current milestone exposes only the `omdi serve` command and unauthenticated `GET /healthz`; media downloads, persistence, authentication, jobs, and extractors are roadmap work.

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

Stop the environment with:

```bash
make compose-down
```

The project-root `.env` configures local Compose development. `OMDI_HTTP_ADDR` defaults to `:8080`; an explicitly empty or invalid address stops startup rather than falling back silently.

## Bruno collection

Open `collection/` in Bruno and select the `local` environment for host-local requests. The collection contains the organized `health/healthz` request and executable assertions.

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
| `make coverage` | Run unit tests and enforce at least 90% coverage. |
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

## Selected versions

- Go 1.27.1
- Chi 5.3.2
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
go run ./cmd/omdi serve
```

The full supported workflow remains Compose-first because it supplies the pinned media tools and quality tooling.

## Current limitations and roadmap

Only liveness is implemented. There is no readiness endpoint, authentication, database, queue, worker, download endpoint, extractor invocation, or iOS Shortcut package.

Phase 0, the project foundation, is complete. Phase 1 has not started; the next development effort is the secure core, beginning with the complete configuration contract. See the ordered delivery status in the [Roadmap](docs/roadmap.md) and the planned system boundaries in [Architecture](docs/architecture.md).

## Public repository safety

Never commit `.env` files, API keys, tokens, cookies, authorization headers, downloaded media, databases, logs, internal paths, or real production URLs. Use only synthetic examples and the tracked `.env.example` files.

## License

This project is available under the [MIT License](LICENSE).
