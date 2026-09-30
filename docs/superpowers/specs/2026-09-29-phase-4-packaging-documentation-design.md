# Phase 4 — Packaging and Documentation Finalization Design

## Context

Phases 0–3 delivered the complete service: authenticated job creation, polling, and
cancellation, a SQLite queue with one worker, real `yt-dlp` and `gallery-dl`
extraction behind a validating egress proxy, short-lived download tokens, cleanup,
and recovery. Later maintenance added trace IDs on every log line, fixed
`error_detail` failure reasons, one retry for transient failures, and opt-in
startup API-key restoration for hosts without persistent storage.

The runtime already exists as the `runtime` stage of the `Dockerfile`, but the only
Compose file is a development environment that compiles the source on every start
and mounts the source tree. A self-hoster has no supported way to run the finished
service, and no document explains how to expose it safely or how to build the iOS
Shortcut that the project exists for.

Phase 4 packages the finished system for self-hosting and finalizes the
documentation against it. It changes no API behavior.

## Goals

- Make `compose.yaml` the production deployment: it builds the runtime image
  locally and runs it with persistent data and the existing hardening and limits.
  The development environment moves to `compose.development.yaml`.
- Keep production and development environments isolated so neither can stop or
  overwrite the other.
- Verify the production Compose path in the required CI suite.
- Document self-hosting end to end: requirements, disk sizing, installation, API
  keys, updates, backups, safe remote access, and operational limits.
- Document how to build the Shortcut, and link to a ready-made example.
- Finalize the README quick start, security notes, OpenAPI contract, and roadmap
  for the completed feature set.

## Non-Goals

- Publishing container images to a registry or a release workflow.
- Bundling a TLS-terminating reverse proxy or tunnel client.
- OpenAPI linting or documentation link checking in CI.
- Any change to HTTP endpoints, job behavior, configuration variables, or limits.
- Provider-specific deployment guides. Documentation stays provider-neutral.
- Committing an exported `.shortcut` file.

## Compose Layout

`compose.yaml` becomes the production deployment, so `docker compose up` in a fresh
checkout runs the service the way self-hosters expect. The current development
environment moves unchanged to `compose.development.yaml`.

- `compose.development.yaml` uses `name: omdi-dev`. Development volumes created under
  the previous project name are not reused; the development database and Go caches
  start empty once. The `Makefile` exports
  `COMPOSE_FILE=compose.development.yaml` unless it is already set, so every
  development target, and the scripts they run, keep using it. Scripts that are
  also run directly default to it the same way.
- `compose.yaml` uses `name: omdi`, so production and development never share
  containers, networks, or volumes.
- One `.env.example` serves both files; there is no separate production example.
  Its comments mark the development-only variables and the production meaning of
  `OMDI_PUBLIC_URL`.

### Production service

- One service, `app`, built from `Dockerfile` with `target: runtime` and tagged
  `omdi:local`, the tag `make docker-build` already produces. It uses the image's
  entrypoint and `serve` command and the image health check.
- Hardening identical to development: `read_only: true`, a `tmpfs` at `/tmp`,
  `cap_drop: [ALL]`, `no-new-privileges:true`, `init: true`, `mem_limit: 1g`, and
  `pids_limit: 64`. It adds `restart: unless-stopped` and `stop_grace_period: 15s`.
- A named volume `omdi-data` mounted at `/data`. Docker initializes it from the
  image's `/data`, owned by the non-root `omdi` user.
- Ports: `${OMDI_PUBLISH_HOST:-127.0.0.1}:8080:8080`, the same variable and loopback
  default as development. The default suits a tunnel or reverse proxy on the same
  host and never exposes plain HTTP to other machines by accident.
- Configuration: `.env` through an optional `env_file`, so a fresh checkout starts
  with defaults and a configured one passes every `OMDI_*` variable to the service.

Self-hosters operate the service with plain `docker compose` commands, so `make` is
not required on the server:

```bash
docker compose up --build --detach --wait
docker compose exec app omdi keys create --name phone
docker compose logs --follow app
docker compose down
```

## Dockerfile

The `runtime` stage gains OCI labels for the source repository, license, title, and
description. No stage, dependency, user, or command changes.

## Production Smoke Test

`make production-smoke` runs `scripts/production-smoke.sh`, and `make ci` runs it
after `smoke`.

- It uses its own project name, `omdi-production-smoke`, and an override,
  `docker/compose.production-smoke.yaml`, that resets the published ports and the
  `env_file`, so it can run beside a development or production instance without port
  conflicts, shared state, or reading the local `.env`. An empty temporary file
  replaces `.env` for interpolation.
- It checks the rendered configuration for the 1 GiB memory and 64-PID limits, the
  read-only root filesystem, and dropped capabilities.
- It starts the stack with `--wait`, checks `/healthz` and `/readyz` from inside the
  container, creates and lists an API key through the CLI without printing the key,
  and removes the project with its volumes on exit, including on failure.

`scripts/check-compose.sh` is generalized to accept Compose file arguments, so the
development and production checks share one implementation.

`make collection-test` passes its temporary API key to Bruno with `--env-var`
through `docker/compose.collection.yaml`. The Bruno container mounts `collection/`,
and a developer's local `collection/.env` would otherwise supply its own key, which
authenticates only against the database that created it.

## Documentation

### `docs/self-hosting.md` (new)

- Requirements: Docker with the Compose plugin. CI verifies x86-64 hosts; every base
  image is multi-architecture, so ARM64 hosts are expected to work but are not
  verified.
- Disk sizing: a job starts only with `OMDI_MIN_FREE_BYTES + 2 × OMDI_MAX_JOB_BYTES`
  free, and completed files stay until `OMDI_JOB_RETENTION`; memory use does not
  depend on file size.
- Installation: clone, copy `.env.example` to `.env`, set `OMDI_PUBLIC_URL`, start,
  and create a key.
- Operation: keys, jobs, logs and log levels, updates (`git pull` and a rebuild),
  and backups of the `omdi-data` volume.
- Remote access: the service speaks plain HTTP; outside a trusted local network it
  must sit behind a tunnel or a TLS reverse proxy, with `OMDI_PUBLIC_URL` set to the
  public HTTPS address and the container port published only on loopback.
- Hosts whose data does not survive restarts: startup key restoration.
- Platform limits: anonymous access, and anti-bot checks that are more frequent
  from datacenter addresses than from residential networks.

### `docs/shortcut.md` (new)

A step-by-step guide for the Shortcuts app: receive a URL from the share sheet,
create a job, poll with `Repeat` and `Wait` because Shortcuts has no conditional
loop, save each item to Photos or Files by media type, and show `error_detail` when
a job fails. It explains the two placeholders, the server URL and the API key, and
links to the example shortcut published through iCloud once its link is available.

### Other documents

- `README.md`: validate the development quick start against the finished system,
  explain the two Compose files, and add a short self-hosting section that points to
  both guides.
- `AGENTS.md`: name both Compose files and the current scope.
- `SECURITY.md`: add deployment notes on TLS outside trusted networks, secret storage
  for keys, sensitivity of debug logs, and keeping pinned dependencies current.
- `docs/architecture.md`: mention the production Compose packaging.
- `docs/openapi.yaml`: review every implemented endpoint and response against the
  handlers and raise `info.version` to `0.4.0`.
- `docs/roadmap.md`: mark Phase 4 `In progress` with a link to this design while it is
  delivered, fix the stale Phase 3 workflow sentence, and mark it `Complete` at the
  end.

## Testing

Phase 4 changes no Go behavior, so it adds no Go tests. Verification is:

- `make production-smoke`, which exercises the production Compose path.
- `make compose-check` for the development file, through the generalized script.
- The complete `make ci` suite, including the Bruno collection and image scan.
- Manual review of every documented command against the files it references.

## Delivery Order

1. Specification and plan.
2. Compose layout, environment example, Dockerfile labels, and the generalized
   Compose check.
3. Production smoke test and its CI integration.
4. Self-hosting and Shortcut guides.
5. README, security notes, architecture, OpenAPI review, and roadmap closure.
6. Complete verification with `make ci`.
