# Self-Hosting

This guide runs OpenMediaDownloaderIOS on your own machine or server with the production Compose file, `compose.yaml`. For development, use the `Makefile` targets described in the [README](../README.md) instead.

## Requirements

- Docker with the Docker Compose plugin.
- An x86-64 host. Every base image is multi-architecture, so ARM64 hosts, such as a Raspberry Pi, are expected to work but are not verified.
- About 1 GiB of memory for the service. Memory use does not grow with file size, because media is written to disk while it downloads.
- Enough disk space for your jobs; see [Disk space](#disk-space).

The first build compiles FFmpeg from source and takes several minutes. Later builds reuse the cache.

## Install and start

```bash
git clone https://github.com/miguins/open-media-downloader-ios.git
cd open-media-downloader-ios
cp .env.example .env
```

Edit `.env` and set `OMDI_PUBLIC_URL` to the address your devices use to reach the service, for example `https://media.example.com`. Download links are built from it, so a wrong value produces links that do not open. The remaining variables are documented in the [README](../README.md#configuration).

```bash
docker compose up --build --detach --wait
docker compose exec app omdi keys create --name phone
```

The second command prints the API key once. Store it in a password manager and enter it in your Shortcut; only its hash is kept.

Check the service:

```bash
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
```

The production project is named `omdi` and never shares containers or volumes with the development environment.

## Prebuilt images

Each [release](https://github.com/miguins/open-media-downloader-ios/releases) publishes the same runtime image for `linux/amd64` and `linux/arm64`:

```text
ghcr.io/miguins/open-media-downloader-ios:<version>
```

Images are tagged only with exact versions such as `0.4.0`; there is no `latest` tag, so an update is always a deliberate change.

To use the image with Docker instead of building locally, create `compose.override.yaml` next to `compose.yaml`. Compose reads it automatically, and it keeps every setting of `compose.yaml` except the build:

```yaml
services:
  app:
    build: !reset null
    image: ghcr.io/miguins/open-media-downloader-ios:0.4.0
```

Then start the service with `docker compose up --detach --wait`. The file is ignored by Git, so updating the checkout never conflicts with it.

Platforms that deploy a prebuilt image can use the same reference. Configure them to:

- route HTTP traffic to container port `8080` and use `/healthz` as the health check;
- set `OMDI_PUBLIC_URL` and the other variables from [Configuration](../README.md#configuration) in their settings;
- mount persistent storage at `/data`, or enable [startup key restoration](#hosts-without-persistent-storage) when none is available.

### Verifying an image

Every published image has a signed build provenance attestation that records the repository, commit, and workflow that built it. Verify it with the GitHub CLI:

```bash
gh attestation verify oci://ghcr.io/miguins/open-media-downloader-ios:0.4.0 --repo miguins/open-media-downloader-ios
```

## Operation

| Task | Command |
| --- | --- |
| Follow logs | `docker compose logs --follow app` |
| List API keys | `docker compose exec app omdi keys list` |
| Revoke an API key | `docker compose exec app omdi keys revoke <id>` |
| Delete revoked keys with their jobs and files | `docker compose exec app omdi keys purge --yes` |
| List jobs | `docker compose exec app omdi jobs list` |
| Delete a job that is not running | `docker compose exec app omdi jobs delete <id>` |
| Stop the service | `docker compose down` |

`docker compose down` keeps the `omdi-data` volume, which holds the database and downloaded files. `docker compose down --volumes` deletes it, including every API key.

### Logs

Logs are JSON lines with a `trace_id` on every line. Every HTTP response carries the same value in its `X-Trace-Id` header, and job lines carry `job_id`, so a failed request or job can be found quickly. `OMDI_LOG_LEVEL=debug` adds sanitized error lines from the media tools; enable it only while investigating a failure, and treat those logs as sensitive. See [Logs](../README.md#logs) for the full description.

### Updates

```bash
git pull
docker compose up --build --detach --wait
```

With a prebuilt image, change the version in `compose.override.yaml` and run `docker compose up --detach --wait`.

Database migrations run automatically at startup. Pinned dependency updates arrive through the repository, so updating regularly keeps `yt-dlp` and `gallery-dl` able to extract platforms that change often.

### Backups

The `omdi-data` volume holds everything worth keeping: the API keys and the jobs still within their retention. Downloaded media is temporary by design. To back up the database, stop the service so the copy is consistent:

```bash
docker compose stop app
docker compose cp app:/data/omdi.db ./omdi.db
docker compose start app
```

To restore, write the file from inside the container, so it belongs to the service user:

```bash
docker compose stop app
docker compose run --rm --no-deps -T --entrypoint sh app -c 'cat > /data/omdi.db' < omdi.db
docker compose start app
```

`docker compose cp` into the container would leave the file owned by your host user, and the service would refuse to start.

## Remote access

The service speaks plain HTTP. On the internet, API keys and download tokens must travel over HTTPS, so never publish port 8080 beyond your machine or a trusted local network. Keep the default loopback binding, `OMDI_PUBLISH_HOST=127.0.0.1`, and put one of these in front of the service on the same host:

- **A tunnel**, such as Cloudflare Tunnel or Tailscale Funnel. It provides HTTPS without opening ports on your router and works behind residential connections.
- **A reverse proxy with TLS**, such as Caddy or nginx, with a certificate for your domain. It needs ports 80 and 443 reachable from the internet.

Point the tunnel or proxy at `http://127.0.0.1:8080` and set `OMDI_PUBLIC_URL` to the resulting public HTTPS address.

To use the service only on a trusted local network, for example from an iPhone at home, set `OMDI_PUBLISH_HOST=0.0.0.0` and `OMDI_PUBLIC_URL` to the machine's local address, such as `http://192.168.1.10:8080`. Traffic is then unencrypted, so do this only on a network you trust.

## Disk space

A job starts only when the data volume has `OMDI_MIN_FREE_BYTES + 2 × OMDI_MAX_JOB_BYTES` free: merging audio and video briefly keeps the input and the output on disk. Completed files stay until `OMDI_JOB_RETENTION` expires, so many successful jobs within one retention window add up. `/readyz` fails when free space drops below `OMDI_MIN_FREE_BYTES`.

With the defaults, 2 GiB per job and 1 GiB reserved, a job needs about 5 GiB free. On small disks, lower `OMDI_MAX_JOB_BYTES` and `OMDI_MIN_FREE_BYTES` together, and shorten `OMDI_JOB_RETENTION` if you save media right after downloading it. A job that fails with `too_large` almost immediately means the disk, not the video, is the limit.

## Hosts without persistent storage

Some hosts discard the container's data on every restart or redeploy, which also discards the API keys. On such hosts, create a key once, store it in the host's secret configuration as `OMDI_API_KEY`, and set `OMDI_RESTORE_API_KEY_ON_STARTUP=true`. The service inserts the key at startup whenever it is missing, so clients keep working. See [Startup key restoration](../README.md#startup-key-restoration) for the rules, including how revocation behaves.

Jobs and downloaded files are still lost on every restart, and in-progress jobs must be submitted again.

## Platform limits

The service never sends cookies or account credentials, so it downloads only what each platform serves to anonymous visitors. Platforms apply anti-bot checks more often to datacenter addresses than to residential networks: the same video can fail with `error_detail` `blocked` on a cloud server and succeed from home. Failures detailed as `forbidden`, `rate_limited`, or `network_error` are retried once automatically. The [README](../README.md#jobs-and-downloads) lists every `error_detail` value.
