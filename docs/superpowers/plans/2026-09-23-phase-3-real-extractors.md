# Phase 3 — Real Extractors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the production fake extractor with bounded real extraction for one public media post per job through `yt-dlp`, `gallery-dl`, `ffprobe`, and FFmpeg.

**Architecture:** Keep the Phase 2 `worker.Extractor` boundary and compose one real extractor from a loopback validating proxy, a bounded subprocess runner, two statically routed downloader adapters, and local-only media inspection/remux helpers. Keep the API and queue lifecycle stable while tightening URL policy to direct-post routes and preserving the fake only in a build-tagged collection-test binary.

**Tech Stack:** Go 1.27.1 standard library, `golang.org/x/net` 0.59.0, Docker Compose, SQLite through `modernc.org/sqlite` 1.59.0, yt-dlp 2026.08.19, gallery-dl 1.32.13, FFmpeg/ffprobe 9.0.2, OpenAPI 3.2.1, Bruno CLI 4.1.0.

**Spec:** `docs/superpowers/specs/2026-09-23-phase-3-real-extractors-design.md`

## Global Constraints

- Read the specification above and `docs/roadmap.md` before starting every task; the spec wins over this plan if wording differs.
- Use Docker Compose and existing `Makefile` targets; do not require host Go, Node.js, Bruno, Python, or media tools.
- Follow red-green-refactor TDD for every behavior change: add one focused failing test, observe the expected failure, add the minimum implementation, and rerun the focused test before broad verification.
- Keep all code, comments, documentation, API descriptions, tests, and messages in English.
- Keep one URL and one public post per job; reject playlists, profiles, channels, feeds, collections, live recording, credentials, cookies, and private-media access.
- Default `OMDI_MAX_JOB_ITEMS` to 20, validate it from 1 through 100, and reject rather than truncate item `max + 1`.
- Prefer H.264/AAC MP4, M4A/AAC, MP3, JPEG, PNG, WebP, and GIF; permit only local stream-copy merge/remux and never transcode.
- Treat submitted URLs, remote data, diagnostics, filenames, metadata, and output files as hostile; trust only the pinned media-tool binaries.
- Never use a shell for submitted input or tool invocation. Use absolute executables, argument arrays, bounded output, context cancellation, and process-group termination.
- Never log or return redirected URLs, headers, cookies, proxy values, command lines, raw diagnostics, internal paths, extracted metadata, or remote response bodies.
- Preserve at least 90% total `internal/...` statement coverage and 100% coverage for `internal/auth`, `internal/storage`, `internal/urlpolicy`, and `internal/extractor`.
- Every changed HTTP behavior must update Go tests, `docs/openapi.yaml`, and the organized Bruno collection in the same task.
- Do not commit or push without explicit user authorization. When a commit is authorized, use the task's conventional message and include `Co-Authored-By: codex <codex@openai.com>` exactly once as the final trailer.
- Run `make ci` before handoff when Docker is available. Do not run the opt-in real network smoke test unless the user supplies `URL` and explicitly asks for it.

## Review Focus

- A supported hostname with encoded separators, dot segments, duplicate identity parameters, or a profile-like path must return `unsupported_url` without creating or notifying a job; Task 2 pins these cases.
- Mixed public/private DNS answers, connect-time rebinding, repeated transfers, and redirected `CONNECT` targets must never reach a private address or escape the per-job budget; Task 3 pins these cases.
- Cancellation or timeout while a tool has a descendant process must terminate the entire process group and leave no diagnostic or path in errors; Task 4 pins this case.
- A carousel containing `MaxItems + 1` entries must fail the whole job as `too_large`, never succeed with a truncated prefix; Task 8 pins this case.
- A file with an allowed extension but malformed content, executable content, unsupported streams, or mismatched probe metadata must never reach storage; Tasks 5 and 6 pin these cases.

---

## File Map

### Configuration, policy, and lifecycle

- Modify `internal/config/config.go` and `internal/config/config_test.go`: add `MaxJobItems` and `OMDI_MAX_JOB_ITEMS` validation.
- Modify `internal/urlpolicy/platform.go`, `internal/urlpolicy/normalize.go`, and `internal/urlpolicy/normalize_test.go`: attach direct-post normalization to each platform.
- Create `internal/urlpolicy/post.go`: platform-specific direct-post path and identifier rules.
- Create `internal/urlpolicy/proxy.go` and `internal/urlpolicy/proxy_test.go`: loopback proxy, active session, validated dialing, and byte budget.
- Modify `internal/worker/worker.go` and `internal/worker/worker_test.go`: pass `MaxItems` and map typed extraction errors.
- Modify `internal/app/app_test.go` only if proxy lifecycle coverage exposes a generic background-task gap; do not change `internal/app/app.go` unless a failing lifecycle test requires it.

### Extractor implementation

- Modify `internal/extractor/extractor.go` and `internal/extractor/fake_test.go`: add request item limit and shared public failure sentinels.
- Create `internal/extractor/runner.go`, `internal/extractor/runner_linux.go`, and `internal/extractor/runner_test.go`: bounded shell-free execution and Linux process-group termination.
- Create `internal/extractor/probe.go` and `internal/extractor/probe_test.go`: bounded `ffprobe` JSON parsing and media classification.
- Create `internal/extractor/ffmpeg.go` and `internal/extractor/ffmpeg_test.go`: local stream-copy remux followed by reinspection.
- Create `internal/extractor/ytdlp.go` and `internal/extractor/ytdlp_test.go`: one-video yt-dlp adapter.
- Create `internal/extractor/gallerydl.go` and `internal/extractor/gallerydl_test.go`: one-post bounded gallery adapter.
- Create `internal/extractor/real.go` and `internal/extractor/real_test.go`: static routing and job-scoped proxy composition.
- Create `internal/extractor/helper_test.go`: one test-process entry point shared by runner and adapter tests.

### Composition, contracts, and operations

- Modify `cmd/omdi/main.go` and `cmd/omdi/main_test.go`: use a build-selected extractor composition and add the proxy background task.
- Create `cmd/omdi/extractor_real.go` and `cmd/omdi/extractor_real_test.go`: normal real composition.
- Create `cmd/omdi/extractor_fake.go` with build tag `omdi_testextractor`: collection-test-only fake composition.
- Create `docker/compose.collection.yaml`: fixed build-tag override used only by the executable collection.
- Modify `compose.yaml`, `.env.example`, `Makefile`, and `scripts/check-coverage.sh`: expose the item limit, add app resource limits, build the fake collection binary, add the real smoke target, and gate extractor coverage.
- Create `scripts/check-compose.sh`: validate rendered app memory and PID limits.
- Create `scripts/real-smoke.sh`: opt-in end-to-end real extraction with trap-based cleanup and no sensitive output.
- Create `scripts/test-real-smoke.sh`: offline fake-Docker harness for smoke cleanup and redaction.
- Modify `internal/api/jobs_test.go`, `docs/openapi.yaml`, `collection/jobs/jobs.yml`, and `collection/jobs/get-job.yml`: synchronize direct-post and carousel/error semantics.
- Modify `README.md`, `SECURITY.md`, `docs/architecture.md`, and `docs/roadmap.md`: document the real runtime, controls, commands, and final phase status.

### Task 1: Item Limit, Request Contract, and Phase Start

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/extractor/extractor.go`
- Modify: `internal/extractor/fake_test.go`
- Modify: `internal/worker/worker.go`
- Modify: `internal/worker/worker_test.go`
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `README.md`
- Modify: `docs/roadmap.md`

**Interfaces:**
- Consumes: existing `config.Load`, `extractor.Request`, and `worker.Settings`.
- Produces: `Config.MaxJobItems int`, `Request.MaxItems int`, and `Settings.MaxJobItems int`; every later extractor task relies on these exact fields.

- [ ] **Step 1: Add failing configuration tests for the item limit**

Add `MaxJobItems: 20` to `defaultConfig`, override it with `"OMDI_MAX_JOB_ITEMS": "100"`, and add invalid table entries for `0`, `101`, `+20`, and a non-number:

```go
func TestLoadValidMaxJobItems(t *testing.T) {
    got, err := Load(lookupFrom(map[string]string{"OMDI_MAX_JOB_ITEMS": "100"}))
    if err != nil || got.MaxJobItems != 100 {
        t.Fatalf("Load() MaxJobItems = %d, %v; want 100", got.MaxJobItems, err)
    }
}
```

- [ ] **Step 2: Run the focused configuration tests and observe failure**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/config -run 'TestLoad(Default|Valid|RejectsInvalid)|TestLoadValidMaxJobItems' -count=1
```

Expected: FAIL because `Config.MaxJobItems` does not exist.

- [ ] **Step 3: Implement the validated configuration field**

Add the field and load it through the existing integer helper:

```go
type Config struct {
    // existing fields
    MaxQueuedJobs int
    MaxJobItems   int
    Tools         Tools
}

MaxJobItems: int(l.integer("OMDI_MAX_JOB_ITEMS", 20, 1, 100)),
```

Update the default and override expectations without changing any other default.

- [ ] **Step 4: Run the configuration package tests**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/config -count=1
```

Expected: PASS.

- [ ] **Step 5: Add a failing worker test for request propagation**

Extend `defaultSettings` and the success assertion:

```go
func defaultSettings() Settings {
    return Settings{JobTimeout: time.Minute, MaxJobBytes: 1 << 20, MinFreeBytes: 0, MaxJobItems: 20}
}

if got.MaxItems != 20 {
    t.Fatalf("extractor MaxItems = %d; want 20", got.MaxItems)
}
```

Update fake-extractor tests to construct requests with `MaxItems: 1`.

- [ ] **Step 6: Run the focused worker test and observe failure**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/worker ./internal/extractor -run 'TestRunOnceSucceeds|TestFake' -count=1
```

Expected: FAIL because `Request.MaxItems` and `Settings.MaxJobItems` do not exist.

- [ ] **Step 7: Extend request and worker settings with the exact approved fields**

```go
type Request struct {
    URL      string
    Platform string
    WorkDir  string
    MaxBytes int64
    MaxItems int
}

type Settings struct {
    JobTimeout   time.Duration
    MaxJobBytes  int64
    MinFreeBytes int64
    MaxJobItems  int
}
```

Set `MaxItems: w.settings.MaxJobItems` when the worker builds the request, and pass `cfg.MaxJobItems` from `cmd/omdi/main.go` when constructing `worker.Settings`.

- [ ] **Step 8: Synchronize configuration surfaces and mark Phase 3 in progress**

Add `OMDI_MAX_JOB_ITEMS:` to the Compose environment, add `# OMDI_MAX_JOB_ITEMS=20` to `.env.example`, add the row to the README configuration table, and change only these roadmap statements:

```markdown
**Phase 3 — Real extractors is in progress. Milestone 1 is active.**
```

```markdown
## Phase 3 — Real extractors

**Status: In progress**
```

Keep Phase 4 `Planned`.

- [ ] **Step 9: Run focused verification**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test ./internal/config ./internal/extractor ./internal/worker ./cmd/omdi -count=1
docker compose config --quiet
```

Expected: all commands succeed.

- [ ] **Step 10: Commit the item-limit foundation when explicitly authorized**

```bash
git add internal/config internal/extractor/extractor.go internal/extractor/fake_test.go internal/worker cmd/omdi/main.go compose.yaml .env.example README.md docs/roadmap.md
git commit -m "feat(config): add extractor item limit" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 2: Direct-Post URL Normalization

**Files:**
- Create: `internal/urlpolicy/post.go`
- Modify: `internal/urlpolicy/platform.go`
- Modify: `internal/urlpolicy/normalize.go`
- Modify: `internal/urlpolicy/normalize_test.go`
- Modify: `internal/api/jobs_test.go`

**Interfaces:**
- Consumes: `urlpolicy.Policy.Normalize(raw string) (Result, error)` and existing platform/domain definitions.
- Produces: `platform.normalize func(*url.URL) error`; `Normalize` returns a canonical direct-post URL or `ErrUnsupportedURL` before job creation.

- [ ] **Step 1: Replace broad accepted-URL cases with the complete direct-post matrix**

Use table rows for every approved shape and canonicalization rule:

```go
tests := []struct{ raw, normalized, platform string }{
    {"https://www.youtube.com/watch?v=abc&list=PL1#t=3", "https://www.youtube.com/watch?v=abc", "youtube"},
    {"https://youtu.be/abc?si=tracking", "https://youtu.be/abc", "youtube"},
    {"https://youtube.com/shorts/a_b-9", "https://youtube.com/shorts/a_b-9", "youtube"},
    {"https://instagram.com/reel/Ab_9/?igsh=x", "https://instagram.com/reel/Ab_9/", "instagram"},
    {"https://www.tiktok.com/@creator/video/123?lang=en", "https://www.tiktok.com/@creator/video/123", "tiktok"},
    {"https://vm.tiktok.com/Ztoken/", "https://vm.tiktok.com/Ztoken/", "tiktok"},
    {"https://twitter.com/user/status/123?s=20", "https://twitter.com/user/status/123", "x"},
    {"https://reddit.com/r/go/comments/abc/title/?share_id=x", "https://reddit.com/r/go/comments/abc/title/", "reddit"},
    {"https://redd.it/abc?utm_source=x", "https://redd.it/abc", "reddit"},
    {"https://vimeo.com/123?share=copy", "https://vimeo.com/123", "vimeo"},
    {"https://player.vimeo.com/video/123", "https://player.vimeo.com/video/123", "vimeo"},
}
```

- [ ] **Step 2: Add the Review Focus rejection matrix**

Add cases for YouTube duplicate or missing `v`, `/playlist`, `/channel`, `/@name`; Instagram profile and `/explore`; TikTok profile and malformed numeric ID; X profile and nonnumeric status; Reddit subreddit/feed without a post; Vimeo channel/showcase; `%2f`, `%2e%2e`, backslash, empty IDs, extra path after fixed-shape IDs, and disallowed-platform direct posts. Assert `errors.Is(err, ErrUnsupportedURL)` and that the input is absent from the error string.

- [ ] **Step 3: Run the URL tests and observe the old broad matcher fail**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/urlpolicy -run 'TestNormalize' -count=1
```

Expected: FAIL because profiles are currently accepted and queries are not canonicalized.

- [ ] **Step 4: Add platform-bound post normalizers**

Change the internal platform definition and matching result:

```go
type platform struct {
    id        string
    domains   []string
    normalize func(*url.URL) error
}

func (p *Policy) match(host string) (platform, bool) {
    for _, candidate := range p.platforms {
        for _, domain := range candidate.domains {
            if host == domain || strings.HasSuffix(host, "."+domain) {
                return candidate, true
            }
        }
    }
    return platform{}, false
}
```

Create focused helpers in `post.go`:

```go
func normalizeYouTube(u *url.URL) error
func normalizeInstagram(u *url.URL) error
func normalizeTikTok(u *url.URL) error
func normalizeX(u *url.URL) error
func normalizeReddit(u *url.URL) error
func normalizeVimeo(u *url.URL) error
```

Each helper must reject `RawPath` encodings that change separators or dot segments, validate ASCII identifiers with anchored regular expressions, preserve an optional trailing slash only where the accepted source form had one, clear `RawQuery` and `ForceQuery`, and let YouTube watch URLs restore exactly `v=<escaped-id>`.

- [ ] **Step 5: Invoke the post normalizer after host matching**

In `Policy.Normalize`, call the matched platform's normalizer before returning:

```go
matched, ok := p.match(host)
if !ok {
    return Result{}, unsupported("host is not an allowed platform")
}
parsed.Host = host
if err := matched.normalize(parsed); err != nil {
    return Result{}, err
}
return Result{URL: parsed.String(), Platform: matched.id}, nil
```

- [ ] **Step 6: Add an API test proving rejection happens before persistence and notification**

Add a profile URL case to `TestCreateJobRejectsInvalidRequests`, then assert both `f.notified == 0` and no job rows exist for the owner. Use `https://vimeo.com/channels/staffpicks` as the profile/collection-shaped input.

- [ ] **Step 7: Run URL policy, API, and full security coverage**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test ./internal/urlpolicy ./internal/api -count=1
make coverage
```

Expected: all commands succeed and `internal/urlpolicy` remains 100% covered.

- [ ] **Step 8: Commit direct-post normalization when explicitly authorized**

```bash
git add internal/urlpolicy internal/api/jobs_test.go
git commit -m "feat(urlpolicy): require direct media posts" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 3: Validating Loopback Egress Proxy

**Files:**
- Create: `internal/urlpolicy/proxy.go`
- Create: `internal/urlpolicy/proxy_test.go`

**Interfaces:**
- Consumes: `Resolver`, `CheckResolved`, `DialControl`, and `IsPublic` from `internal/urlpolicy`.
- Produces:

```go
var ErrProxyBusy error
var ErrEgressTooLarge error

type Dialer interface {
    DialContext(context.Context, string, string) (net.Conn, error)
}

func NewProxy(resolver Resolver, dialer Dialer) (*Proxy, error)
func EgressBudget(maxJobBytes int64) int64
func (p *Proxy) Run(ctx context.Context) error
func (p *Proxy) Begin(ctx context.Context, maxBytes int64) (*ProxySession, error)
func (s *ProxySession) URL() string
func (s *ProxySession) Err() error
func (s *ProxySession) Close() error
```

- [ ] **Step 1: Add failing constructor, lifecycle, and budget tests**

Test that `NewProxy` binds an IPv4 loopback ephemeral address, `URL()` begins with `http://127.0.0.1:`, requests without an active session receive 503, `Begin` rejects a second active session with `ErrProxyBusy`, closing a session closes its tunnels, canceling `Run` returns nil, and:

```go
func TestEgressBudget(t *testing.T) {
    if got := EgressBudget(1 << 20); got != 2<<20 {
        t.Fatalf("EgressBudget(1 MiB) = %d; want 2 MiB", got)
    }
    if got := EgressBudget(100 << 20); got != 105<<20 {
        t.Fatalf("EgressBudget(100 MiB) = %d; want 105 MiB", got)
    }
}
```

- [ ] **Step 2: Run proxy tests and observe missing symbols**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/urlpolicy -run 'Test(Egress|Proxy)' -count=1
```

Expected: FAIL because the proxy API does not exist.

- [ ] **Step 3: Implement the listener, single active session, and graceful lifecycle**

Use `net.ListenConfig.Listen(context.Background(), "tcp4", "127.0.0.1:0")`, an `http.Server` with bounded header timeouts, a mutex-protected active session, and connection tracking owned by that session. `Run` serves until context cancellation, then calls `Shutdown` and normalizes `http.ErrServerClosed` to nil. `Close` clears only its own session and closes tracked upstream/client connections.

- [ ] **Step 4: Add failing HTTP forwarding validation tests**

Use a fake resolver returning `93.184.216.34` and a fake dialer that records the requested address but connects to a local `httptest.Server`. Cover GET success, response header/body forwarding, port 80 defaulting, user info rejection, methods with request bodies, unsupported scheme, port 22, malformed authority, DNS failure, no addresses, mixed public/private addresses, response headers above 64 KiB, and errors that do not include target host or path.

- [ ] **Step 5: Implement a validating transport for ordinary HTTP requests**

Before dialing, split host and port, require port 80 or 443, call `CheckResolved`, and use a transport whose `Proxy` is nil, whose `DialContext` is the injected dialer, and whose `MaxResponseHeaderBytes` is 64 KiB. Clone the inbound request, clear `RequestURI`, remove hop-by-hop and proxy authorization headers, and never call `http.DefaultTransport`. Charge the serialized status line and response headers to the session before writing them, then stream the body through the same counting writer.

- [ ] **Step 6: Add failing HTTPS CONNECT and rebinding tests**

Cover a successful CONNECT tunnel, non-CONNECT tunnel attempts, port 443 defaulting, port 8443 rejection, mixed DNS rejection before the dialer runs, injected dialer failure representing connect-time `DialControl`, client cancellation, two sequential CONNECT destinations representing a redirect, and the second destination resolving private.

- [ ] **Step 7: Implement CONNECT tunneling without buffering payloads**

Hijack only after validation and upstream connection success. Return `HTTP/1.1 200 Connection Established`, copy client-to-upstream without charging the response budget, copy upstream-to-client through the session counter, and close both halves when either copy, session context, or application context ends. Register both connections with the session before starting copies.

- [ ] **Step 8: Add failing accounting and retry tests**

Send two upstream responses whose combined bodies cross the session limit. Assert every repeated byte counts, `ProxySession.Err()` becomes `ErrEgressTooLarge`, the active connection closes, later requests fail, and exact-limit traffic succeeds. Repeat through CONNECT with opaque bytes to prove encrypted tunnels are counted.

- [ ] **Step 9: Implement the atomic session byte counter**

Use a mutex or atomic compare-and-swap loop that refuses a write crossing the remaining budget, records `ErrEgressTooLarge` once, cancels the session, and never returns bytes beyond the limit. Implement the exact formula:

```go
func EgressBudget(maxJobBytes int64) int64 {
    margin := maxJobBytes / 20
    if margin < 1<<20 {
        margin = 1 << 20
    }
    return maxJobBytes + margin
}
```

- [ ] **Step 10: Verify the production dialer defeats rebinding**

Add a test that composes `&net.Dialer{Control: DialControl}` against a loopback listener after a resolver reports a public address. Assert the actual dial is rejected with `ErrNonPublicAddress`; do not weaken `DialControl` for tests.

- [ ] **Step 11: Run race, coverage, and leak-focused tests**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test -race ./internal/urlpolicy -count=1
make coverage
```

Expected: PASS, no race report, and `internal/urlpolicy` at 100%.

- [ ] **Step 12: Commit the egress proxy when explicitly authorized**

```bash
git add internal/urlpolicy/proxy.go internal/urlpolicy/proxy_test.go
git commit -m "feat(urlpolicy): add bounded egress proxy" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 4: Bounded Subprocess Runner and Extraction Errors

**Files:**
- Create: `internal/extractor/runner.go`
- Create: `internal/extractor/runner_linux.go`
- Create: `internal/extractor/runner_test.go`
- Create: `internal/extractor/helper_test.go`
- Modify: `internal/extractor/extractor.go`

**Interfaces:**
- Consumes: validated absolute tool paths and a job context.
- Produces:

```go
var ErrTooLarge error
var ErrExtractionFailed error
var ErrCommandExit error
var ErrOutputLimit error

type Command struct {
    Path        string
    Args        []string
    Dir         string
    StdoutLimit int64
    StderrLimit int64
}

type Result struct {
    Stdout   []byte
    Stderr   []byte
    ExitCode int
}

type Runner struct {
    grace    time.Duration
    extraEnv []string // test helper activation only; nil in production
}
func NewRunner() *Runner
func (r *Runner) Run(ctx context.Context, command Command) (Result, error)
```

- [ ] **Step 1: Add a shared helper-process entry point**

Implement `TestExtractorHelperProcess` guarded by `GO_WANT_EXTRACTOR_HELPER=1`. Subcommands must print argv/env/cwd, emit requested stdout/stderr byte counts, sleep until signaled, exit with a requested code, and spawn one sleeping child whose PID is written to a test-supplied file. The helper is test code only and never accepts submitted application input.

- [ ] **Step 2: Add failing validation and success tests**

Test rejection of relative or unclean executable paths, empty/non-absolute work directories, nonpositive output limits, and a canceled context. Test that success preserves each argument literally, sets cwd, closes stdin, and exposes only this fixed environment:

```text
HOME=<work>/.omdi-runtime/home
XDG_CACHE_HOME=<work>/.omdi-runtime/cache
TMPDIR=<work>/.omdi-runtime/tmp
LANG=C.UTF-8
LC_ALL=C.UTF-8
PATH=/usr/bin:/bin
GO_WANT_EXTRACTOR_HELPER=1   # test factory only
```

Assert parent proxy variables, secrets, and arbitrary environment variables are absent.

- [ ] **Step 3: Run runner tests and observe missing symbols**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/extractor -run 'TestRunner' -count=1
```

Expected: FAIL because `Runner` does not exist.

- [ ] **Step 4: Implement command validation, isolated runtime directories, and success execution**

Create `.omdi-runtime/{home,cache,tmp}` with mode `0700`, build `exec.Cmd` directly with `command.Path` and `command.Args`, set `Dir`, the fixed environment, and an empty stdin reader, and remove `.omdi-runtime` after `Wait` on every path. Never include an executable path, argument, cwd, env value, or captured output in a returned error.

- [ ] **Step 5: Add failing exit and output-limit tests**

Cover exit 7 as `ErrCommandExit` with `ExitCode == 7`, launch failure as an internal sanitized error, exact stdout/stderr limits succeeding, one byte over either limit returning `ErrOutputLimit`, and captured buffers never exceeding their configured limits. Put a synthetic URL and filesystem path in stderr and assert neither appears in `err.Error()`.

- [ ] **Step 6: Implement capped capture and sanitized exit classification**

Use separate capped writers for stdout and stderr. On overflow, cancel the child run context, return `ErrOutputLimit`, and retain at most the configured prefix for in-memory parsing. Wrap only stable sentinels and static messages. Set `ExitCode` from `*exec.ExitError`; use `-1` when the process never starts.

- [ ] **Step 7: Add the Review Focus process-tree cancellation tests**

Start the helper with a sleeping descendant, cancel the context, and assert both parent and child PIDs disappear. Repeat with a deadline and with output overflow. Assert the runner returns within the configured 500 ms test grace period and that `.omdi-runtime` is removed.

- [ ] **Step 8: Implement Linux process-group termination**

In `runner_linux.go`, set `SysProcAttr.Setpgid = true`. On cancellation send `SIGTERM` to `-pid`, wait for the runner's grace period, then send `SIGKILL` to `-pid`. Treat `ESRCH` as already stopped. Keep this logic behind small private functions so every branch can be tested without exposing process details.

- [ ] **Step 9: Add shared extraction sentinels**

In `extractor.go` define stable errors without diagnostic payloads:

```go
var (
    ErrTooLarge        = errors.New("extractor: resource limit exceeded")
    ErrExtractionFailed = errors.New("extractor: extraction failed")
)
```

Adapters will wrap these sentinels with static context only. Runner launch and local I/O failures do not wrap either sentinel, so the worker can classify them as internal.

- [ ] **Step 10: Run race tests and coverage for the runner**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test -race ./internal/extractor -run 'TestRunner|TestFake' -count=1
```

Expected: PASS with no leftover helper processes.

- [ ] **Step 11: Commit the runner when explicitly authorized**

```bash
git add internal/extractor
git commit -m "feat(extractor): add bounded process runner" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 5: Local Media Inspection with `ffprobe`

**Files:**
- Create: `internal/extractor/probe.go`
- Create: `internal/extractor/probe_test.go`
- Modify: `internal/extractor/helper_test.go`

**Interfaces:**
- Consumes: `Runner.Run` and server-selected plain entry names under a job work directory.
- Produces:

```go
type Inspection struct {
    Name        string
    MediaType   string
    Format      string
    VideoCodec  string
    AudioCodec  string
    NeedsRemux  bool
}

type Probe struct {
    path   string
    runner *Runner
}
func NewProbe(path string, runner *Runner) *Probe
func (p *Probe) Inspect(ctx context.Context, workDir, name string) (Inspection, error)
```

- [ ] **Step 1: Extend the helper process to emulate bounded `ffprobe` JSON**

Add helper modes that emit caller-selected JSON, malformed JSON, more than 256 KiB, a non-zero exit with a synthetic signed URL, and no output. Keep the executable path as `os.Args[0]` through the runner's test factory so production code still validates absolute paths.

- [ ] **Step 2: Add failing command-shape and path-safety tests**

Assert the probe invokes this argument shape with the local absolute input last:

```text
-v error
-nostdin
-protocol_whitelist file
-show_format
-show_streams
-of json
<absolute-work-path>/<plain-name>
```

Reject empty names, `.`, `..`, separators, absolute names, missing entries, directories, symlinks, and non-regular files before execution. Assert errors contain neither work path nor name.

- [ ] **Step 3: Run probe tests and observe missing symbols**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/extractor -run 'TestProbe' -count=1
```

Expected: FAIL because `Probe` and `Inspection` do not exist.

- [ ] **Step 4: Implement safe invocation and a closed JSON schema**

Decode only these fields and disallow trailing JSON values:

```go
type probeDocument struct {
    Streams []struct {
        CodecType string `json:"codec_type"`
        CodecName string `json:"codec_name"`
    } `json:"streams"`
    Format struct {
        FormatName string `json:"format_name"`
        Tags struct {
            MajorBrand string `json:"major_brand"`
        } `json:"tags"`
    } `json:"format"`
}
```

Use `StdoutLimit: 256 << 10` and `StderrLimit: 64 << 10`. A command exit, empty output, malformed document, unknown fields required for classification, or unsupported content wraps `ErrExtractionFailed`; launch and local filesystem failures remain internal sanitized errors.

- [ ] **Step 5: Add failing classification tests for every accepted final type**

Use representative documents for:

```text
mov,mp4,m4a,3gp,3g2,mj2 + h264 + aac => video/mp4
mov,mp4,m4a,3gp,3g2,mj2 + aac only   => audio/mp4
mp3 + mp3                             => audio/mpeg
image2/jpeg_pipe + mjpeg              => image/jpeg
image2/png_pipe + png                 => image/png
image2/webp_pipe + webp               => image/webp
gif/image2 + gif                      => image/gif
mov-family + major_brand "qt  " + h264 + aac => video/quicktime
mpegts + h264 + optional aac          => NeedsRemux true
```

Assert video with no audio remains valid, audio with no video follows the audio mapping, and more than one media stream of the same type is rejected rather than guessed.

- [ ] **Step 6: Add the Review Focus invalid-content matrix**

Cover allowed extensions containing HTML, ELF/executable bytes as reported by an unsupported probe document, subtitle-only, attachment/data streams, VP9/Opus WebM, AV1, unknown codec, unknown container, no streams, conflicting codecs, malformed JSON, trailing JSON, oversized JSON, and a file extension that disagrees with the inspected content. The extension never overrides probe output.

- [ ] **Step 7: Implement deterministic media classification**

Split comma-separated `format_name` into an exact set and classify from the stream types and codecs. Return only approved media types. Set `NeedsRemux` only for H.264 with optional AAC in a non-MP4 local container that FFmpeg can copy; do not mark incompatible codecs for remux because that would require transcoding.

- [ ] **Step 8: Run probe tests, race tests, and package coverage**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test -race ./internal/extractor -run 'TestProbe|TestFake|TestRunner' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit media inspection when explicitly authorized**

```bash
git add internal/extractor/probe.go internal/extractor/probe_test.go internal/extractor/helper_test.go
git commit -m "feat(extractor): inspect local media outputs" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 6: Local Stream-Copy Remux and Finalization

**Files:**
- Create: `internal/extractor/ffmpeg.go`
- Create: `internal/extractor/ffmpeg_test.go`
- Modify: `internal/extractor/helper_test.go`

**Interfaces:**
- Consumes: `Probe.Inspect`, `Inspection.NeedsRemux`, and `Runner.Run`.
- Produces:

```go
type MediaTools struct {
    probe      *Probe
    ffmpegPath string
    runner     *Runner
}
func NewMediaTools(ffprobePath, ffmpegPath string, runner *Runner) *MediaTools
func (m *MediaTools) Finalize(ctx context.Context, workDir string, names []string) ([]File, error)
```

- [ ] **Step 1: Add failing pass-through finalization tests**

For valid MP4, M4A, MP3, JPEG, PNG, WebP, GIF, and QuickTime helper outputs, assert `Finalize` preserves input order, calls only ffprobe, and returns the inspected `MediaType` rather than any extension-derived type.

- [ ] **Step 2: Add failing input-set safety tests**

Reject zero names, duplicates, more names than the caller supplied limit in adapter tests, unsafe names, missing entries, and any probe failure. Assert partial success never returns files.

- [ ] **Step 3: Run finalizer tests and observe missing symbols**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/extractor -run 'TestMediaTools' -count=1
```

Expected: FAIL because `MediaTools` does not exist.

- [ ] **Step 4: Implement ordered inspection and pass-through results**

Construct one `Probe` in `NewMediaTools`. Inspect names sequentially under the job context and build `[]File{{Name: inspection.Name, MediaType: inspection.MediaType}}`. Return no partial slice on the first failure.

- [ ] **Step 5: Add failing FFmpeg argument and replacement tests**

For an MPEG-TS H.264/AAC candidate, assert this exact semantic argument sequence:

```text
-v error -nostdin -n -protocol_whitelist file
-i <local-input>
-map 0:v:0? -map 0:a:0?
-c copy -movflags +faststart
<fresh-local-output>.mp4
```

Have the helper write a valid output only when arguments are correct. Assert the output is probed again, the original is removed only after successful reinspection, and the returned name/media type point to the MP4 replacement.

- [ ] **Step 6: Implement fresh-name stream-copy remux**

Use a server-generated plain name such as `.omdi-remux-001.mp4`, require it not to exist, run FFmpeg with `StdoutLimit: 64 << 10` and `StderrLimit: 64 << 10`, inspect it, require `video/mp4` and `NeedsRemux == false`, then remove the original and rename the remuxed file to `media-001.mp4`. Every path is built from the validated work directory and server-generated names.

- [ ] **Step 7: Add failing cleanup and no-transcode tests**

Cover FFmpeg non-zero exit, launch failure, context cancellation, oversized diagnostics, missing output, symlink output, malformed output, output that still needs remux, output with VP9/Opus, second-probe failure, original-removal failure, and destination collision. Assert temporary outputs are removed, original inputs remain until validation succeeds, and no encoder other than `copy` appears in argv.

- [ ] **Step 8: Implement transactional cleanup for every remux failure**

Defer removal of the server-generated temporary path, clear that cleanup only after validated replacement, and wrap tool/content failures with `ErrExtractionFailed`. Keep local I/O and launch failures internal. Never include argv, names, or paths in errors.

- [ ] **Step 9: Run extractor race tests**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test -race ./internal/extractor -run 'Test(MediaTools|FFmpeg|Probe|Runner)' -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit local remux when explicitly authorized**

```bash
git add internal/extractor/ffmpeg.go internal/extractor/ffmpeg_test.go internal/extractor/helper_test.go
git commit -m "feat(extractor): add local stream-copy remux" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 7: `yt-dlp` Adapter

**Files:**
- Create: `internal/extractor/ytdlp.go`
- Create: `internal/extractor/ytdlp_test.go`
- Modify: `internal/extractor/helper_test.go`

**Interfaces:**
- Consumes: `Request`, `Runner`, `MediaTools`, a job-scoped proxy URL, and configured absolute yt-dlp/FFmpeg paths.
- Produces:

```go
type YTDLP struct {
    path       string
    ffmpegPath string
    runner     *Runner
    media      *MediaTools
}
func NewYTDLP(path, ffmpegPath string, runner *Runner, media *MediaTools) *YTDLP
func (y *YTDLP) Extract(ctx context.Context, request Request, proxyURL string) ([]File, error)
```

- [ ] **Step 1: Add a failing exact-argument test**

Have the helper capture argv and create `media.mp4`. Assert the fixed options occur before `--` and the submitted URL is exactly one final argument after `--`:

```text
--ignore-config --no-config-locations --no-cache-dir
--no-plugin-dirs --no-remote-components
--abort-on-error --no-playlist --max-downloads 1
--match-filters !is_live
--concurrent-fragments 1 --retries 3 --fragment-retries 3 --file-access-retries 1
--socket-timeout 30
--no-write-comments --no-write-info-json --no-write-playlist-metafiles
--no-write-thumbnail --no-write-subs --no-progress --color never
--ies default,-generic
--proxy <job-proxy-url>
--ffmpeg-location <absolute-ffmpeg-path>
--max-filesize <request.MaxBytes decimal>
--paths home:<workdir> --paths temp:<workdir>/.omdi-runtime/tmp
--output media.%(ext)s
--format bestvideo[vcodec^=avc1]+bestaudio[acodec^=mp4a]/best[vcodec^=avc1][acodec^=mp4a]/bestvideo[vcodec^=avc1]/bestaudio[ext=m4a]/bestaudio[ext=mp3]
--merge-output-format mp4
--print after_move:filepath
--
<request.URL>
```

Assert no cookie, browser, netrc, username, password, update, external downloader, file-URL, arbitrary header, or geo-bypass flag appears.

- [ ] **Step 2: Run the focused adapter test and observe missing symbols**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/extractor -run 'TestYTDLPArguments' -count=1
```

Expected: FAIL because `YTDLP` does not exist.

- [ ] **Step 3: Implement validation and the fixed command builder**

Require platform `youtube`, `vimeo`, or `tiktok`; a nonempty normalized HTTPS URL; positive `MaxBytes` and `MaxItems`; an absolute clean work directory; and an `http://127.0.0.1:<port>` proxy URL. Build a new argument slice for every call and invoke `Runner` with stdout 256 KiB and stderr 64 KiB.

- [ ] **Step 4: Add failing output-discovery tests**

Cover one final regular file, no output, two final files, a symlink, hard link, FIFO, nested directory, `.part`, `.ytdl`, info JSON, thumbnail, subtitle, and an unexpected plain file. Print a believable but false path on stdout and prove it is ignored. The known `.omdi-runtime` directory is removed by `Runner`; any other directory is invalid.

- [ ] **Step 5: Implement top-level output enumeration**

After exit success, call `os.ReadDir(request.WorkDir)`, require exactly one non-hidden plain entry named `media.<extension>`, use `Lstat` to require a regular file with one link, and pass that plain name to `MediaTools.Finalize`. Never open or trust the printed after-move path.

- [ ] **Step 6: Add failing error and limit classification tests**

Cover non-zero tool exit with a private URL in stderr as `ErrExtractionFailed`, output limit as `ErrExtractionFailed`, `MediaTools` unsupported content as `ErrExtractionFailed`, context cancellation preserving `context.Canceled`, context deadline preserving `context.DeadlineExceeded`, and runner launch/local filesystem failure remaining internal. Assert no captured data reaches errors.

- [ ] **Step 7: Implement sanitized adapter classification**

Map `ErrCommandExit`, `ErrOutputLimit`, invalid output discovery, and media validation to `ErrExtractionFailed`. Return context errors unchanged so the worker's timeout/cancellation logic remains authoritative. Propagate internal runner/filesystem errors without adding paths or diagnostics.

- [ ] **Step 8: Run all yt-dlp and dependency tests**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test -race ./internal/extractor -run 'Test(YTDLP|MediaTools|FFmpeg|Probe|Runner)' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit the yt-dlp adapter when explicitly authorized**

```bash
git add internal/extractor/ytdlp.go internal/extractor/ytdlp_test.go internal/extractor/helper_test.go
git commit -m "feat(extractor): add yt-dlp adapter" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 8: `gallery-dl` Adapter and Carousel Bounds

**Files:**
- Create: `internal/extractor/gallerydl.go`
- Create: `internal/extractor/gallerydl_test.go`
- Modify: `internal/extractor/helper_test.go`

**Interfaces:**
- Consumes: `Request`, `Runner`, `MediaTools`, a job-scoped proxy URL, and the configured gallery-dl path.
- Produces:

```go
type GalleryDL struct {
    path   string
    runner *Runner
    media  *MediaTools
}
func NewGalleryDL(path string, runner *Runner, media *MediaTools) *GalleryDL
func (g *GalleryDL) Extract(ctx context.Context, request Request, proxyURL string) ([]File, error)
```

- [ ] **Step 1: Add a failing exact-argument test**

Assert this semantic argument list, followed by `--` and the submitted URL:

```text
--config-ignore
--no-input
--no-colors
--no-postprocessors
--no-mtime
--proxy <job-proxy-url>
--directory <workdir>
--filename item-{num:03}.{extension}
--range 1-<MaxItems+1>
--filesize-max <request.MaxBytes decimal>
--retries 3
--http-timeout 30
-o cache.file=:memory:
--
<request.URL>
```

Assert no cookies, browser import, archive, metadata writer, traffic dump, intermediary-page dump, ZIP/CBZ, external input file, or postprocessor option appears. The child environment's isolated cache is still required even with the in-memory cache setting.

- [ ] **Step 2: Run the focused adapter test and observe missing symbols**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/extractor -run 'TestGalleryDLArguments' -count=1
```

Expected: FAIL because `GalleryDL` does not exist.

- [ ] **Step 3: Implement validation and fixed command construction**

Require platform `instagram`, `x`, or `reddit`, positive byte/item limits, the same work-directory and loopback-proxy validation as yt-dlp, and compute the inclusive range with `strconv.Itoa(request.MaxItems + 1)`. Use stdout 256 KiB and stderr 64 KiB.

- [ ] **Step 4: Add ordered discovery and the max-plus-one Review Focus tests**

Have the helper produce `item-001.jpg`, `item-002.mp4`, and so on. Assert lexical numeric order is source order, gaps or duplicates fail, zero outputs fail, exactly `MaxItems` succeeds, and `MaxItems + 1` returns `ErrTooLarge` with no partial files. Set `MaxItems` to 1 in one test to catch off-by-one logic.

- [ ] **Step 5: Implement numbered top-level enumeration**

Accept only anchored names `^item-[0-9]{3}\.[A-Za-z0-9]+$`, sort by the numeric component, require positions start at 1 without gaps, and use `Lstat` plus link-count validation. Check for `len(names) > request.MaxItems` before probing and return `ErrTooLarge` without a result slice.

- [ ] **Step 6: Add hostile-output and error-classification tests**

Cover symlink, hard link, FIFO, directory, nested output, partial file, metadata sidecar, unexpected name, unsupported media, non-zero exit, output overflow, canceled context, deadline, and internal launch failure. Include diagnostics with a signed URL and assert errors remain sanitized.

- [ ] **Step 7: Implement finalization and error mapping**

Pass the ordered names to `MediaTools.Finalize`. Map command exits, output overflow, unsafe output, and media validation to `ErrExtractionFailed`; map max-plus-one to `ErrTooLarge`; preserve context errors and internal failures exactly as in the yt-dlp adapter.

- [ ] **Step 8: Run all gallery and extractor dependency tests**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test -race ./internal/extractor -run 'Test(GalleryDL|YTDLP|MediaTools|FFmpeg|Probe|Runner)' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit the gallery adapter when explicitly authorized**

```bash
git add internal/extractor/gallerydl.go internal/extractor/gallerydl_test.go internal/extractor/helper_test.go
git commit -m "feat(extractor): add gallery-dl adapter" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 9: Real Extractor Routing, Worker Mapping, and Build-Selected Composition

**Files:**
- Create: `internal/extractor/real.go`
- Create: `internal/extractor/real_test.go`
- Modify: `internal/worker/worker.go`
- Modify: `internal/worker/worker_test.go`
- Modify: `cmd/omdi/main.go`
- Modify: `cmd/omdi/main_test.go`
- Create: `cmd/omdi/extractor_real.go`
- Create: `cmd/omdi/extractor_real_test.go`
- Create: `cmd/omdi/extractor_fake.go`

**Interfaces:**
- Consumes: `urlpolicy.Proxy`, `YTDLP`, `GalleryDL`, typed extractor errors, configuration tool paths, and `app.Run` background functions.
- Produces:

```go
type proxySession interface {
    URL() string
    Err() error
    Close() error
}

type beginSessionFunc func(context.Context, int64) (proxySession, error)
type adapterFunc func(context.Context, Request, string) ([]File, error)

type Real struct {
    beginSession beginSessionFunc
    ytdlp        adapterFunc
    gallery      adapterFunc
}
func NewReal(proxy *urlpolicy.Proxy, ytdlp *YTDLP, gallery *GalleryDL) *Real
func (r *Real) Extract(ctx context.Context, request Request) ([]File, error)

type extractorComponents struct {
    extractor worker.Extractor
    run       func(context.Context) error
}

func newExtractorComponents(cfg config.Config) (extractorComponents, error)
```

- [ ] **Step 1: Add failing static-routing tests**

Construct `Real` in package tests with `beginSession`, `ytdlp`, and `gallery` function fields that record invocation. Assert YouTube, Vimeo, and TikTok invoke only yt-dlp; Instagram, X, and Reddit invoke only gallery-dl; an unknown platform returns a sanitized internal error and invokes neither. A failed selected adapter must not invoke the other adapter.

- [ ] **Step 2: Add failing proxy-session precedence tests**

Assert every extraction begins one session using `request.MaxBytes`, closes it on success and every error, rejects concurrent extraction with a sanitized internal error, and maps `ProxySession.Err() == urlpolicy.ErrEgressTooLarge` to `extractor.ErrTooLarge` even when the selected adapter reports a command exit. A canceled job context still returns `context.Canceled`; a deadline still returns `context.DeadlineExceeded`.

- [ ] **Step 3: Run real-extractor tests and observe missing symbols**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/extractor -run 'TestReal' -count=1
```

Expected: FAIL because `Real` does not exist.

- [ ] **Step 4: Implement the concrete router and session ownership**

`NewReal` initializes the private function fields with a wrapper around `proxy.Begin`, `ytdlp.Extract`, and `gallery.Extract`. `Real.Extract` validates the request, calls `r.beginSession(ctx, request.MaxBytes)`, defers `session.Close`, switches on `request.Platform`, and passes `session.URL()` to exactly one adapter. After the adapter returns, check context cause first, then `session.Err`, then the adapter error. Never include the URL or proxy address in errors.

- [ ] **Step 5: Add failing worker error-mapping tests**

Extend `TestRunOnceFailures` with extractors returning wrapped `extractor.ErrTooLarge`, wrapped `extractor.ErrExtractionFailed`, and a plain internal error. Expected codes are `too_large`, `extraction_failed`, and `internal`. Keep the existing timeout, cancellation, storage-invalid-output, and disk-space cases unchanged. Assert logs contain platform and normalized code but no wrapped diagnostic.

- [ ] **Step 6: Implement typed mapping in the worker**

Use this precedence after context timeout/cancellation handling:

```go
switch {
case errors.Is(err, extractor.ErrTooLarge):
    w.fail(ctx, j, job.ErrorTooLarge)
case errors.Is(err, extractor.ErrExtractionFailed):
    w.fail(ctx, j, job.ErrorExtractionFailed)
case err != nil:
    w.fail(ctx, j, job.ErrorInternal)
default:
    w.complete(ctx, j, files)
}
```

Add `"platform", j.Platform` to the existing normalized failure log; do not log `err`.

- [ ] **Step 7: Add failing normal-composition tests**

Test `newExtractorComponents` with temporary absolute helper paths injected through `Config.Tools`. Assert it returns `*extractor.Real`, its background function runs the proxy until context cancellation, and construction rejects a proxy-listener failure without leaking an address or path. Test that `serve` passes the proxy background before worker and cleaner without changing app shutdown semantics.

- [ ] **Step 8: Implement normal real composition**

In `extractor_real.go` under `//go:build !omdi_testextractor`, construct:

```go
runner := extractor.NewRunner()
media := extractor.NewMediaTools(cfg.Tools.FFprobe, cfg.Tools.FFmpeg, runner)
proxy, err := urlpolicy.NewProxy(net.DefaultResolver, &net.Dialer{Control: urlpolicy.DialControl})
ytdlp := extractor.NewYTDLP(cfg.Tools.YTDLP, cfg.Tools.FFmpeg, runner, media)
gallery := extractor.NewGalleryDL(cfg.Tools.GalleryDL, runner, media)
real := extractor.NewReal(proxy, ytdlp, gallery)
```

Return `extractorComponents{extractor: real, run: proxy.Run}`. In `serve`, build components before the worker, wire `components.extractor`, and call `app.Run(..., components.run, jobWorker.Run, cleaner.Run)`.

- [ ] **Step 9: Add the build-tagged fake composition**

Create `extractor_fake.go` with:

```go
//go:build omdi_testextractor

func newExtractorComponents(config.Config) (extractorComponents, error) {
    return extractorComponents{
        extractor: extractor.Fake{},
        run: func(ctx context.Context) error {
            <-ctx.Done()
            return nil
        },
    }, nil
}
```

Do not add a runtime environment variable or CLI flag for fake extraction.

- [ ] **Step 10: Verify both build selections**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test -race ./internal/extractor ./internal/worker ./cmd/omdi -count=1
docker compose run --rm --no-deps tools go test -race -tags=omdi_testextractor ./cmd/omdi -count=1
docker compose run --rm --no-deps tools go build -trimpath -o /tmp/omdi-real ./cmd/omdi
docker compose run --rm --no-deps tools go build -tags=omdi_testextractor -trimpath -o /tmp/omdi-fake ./cmd/omdi
```

Expected: all commands succeed.

- [ ] **Step 11: Commit production composition when explicitly authorized**

```bash
git add internal/extractor/real.go internal/extractor/real_test.go internal/worker cmd/omdi
git commit -m "feat(app): wire real platform extractors" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 10: API Contract, Bruno Test Build, Compose Limits, and Coverage Gate

**Files:**
- Modify: `internal/api/jobs_test.go`
- Modify: `docs/openapi.yaml`
- Modify: `collection/jobs/jobs.yml`
- Modify: `collection/jobs/get-job.yml`
- Modify: `compose.yaml`
- Create: `docker/compose.collection.yaml`
- Modify: `Makefile`
- Modify: `scripts/check-coverage.sh`
- Create: `scripts/check-compose.sh`

**Interfaces:**
- Consumes: direct-post normalization, build tag `omdi_testextractor`, and existing collection workflow.
- Produces: OpenAPI `0.3.0`, deterministic offline collection execution, Compose 1 GiB/64 PID limits, and a 100% extractor coverage gate.

- [ ] **Step 1: Add API contract tests for canonical direct posts and multi-item responses**

Extend API tests so a YouTube watch URL with playlist context stores only `?v=...`, a profile-shaped supported-host URL returns `422 unsupported_url` without a row, and a succeeded job with two ingested image items returns both in position order with distinct current download tokens. Do not add a batch request shape.

- [ ] **Step 2: Run the focused API tests**

Run:

```bash
docker compose run --rm --no-deps tools go test ./internal/api -run 'TestCreateJob|TestSucceededJob' -count=1
```

Expected: PASS after Tasks 2 and 9; any mismatch must be fixed before editing contracts.

- [ ] **Step 3: Update OpenAPI and Bruno together**

Set `info.version: 0.3.0`. Update the `POST /v1/jobs` URL description to "one public post on an allowed platform; profiles, channels, playlists, feeds, and collections are rejected." State that a succeeded carousel job has several ordered items and retain the existing error-code set. Keep all operation IDs, paths, and request shapes unchanged.

Keep the Bruno request URL `https://vimeo.com/76979871`. Update its docs to say "one public post" and keep the executable fake-build assertion at exactly one synthetic item. Multi-item contract behavior remains covered by the Go API test and OpenAPI schema. Do not add a real platform request to the required collection.

- [ ] **Step 4: Add failing Compose resource-limit validation**

Create `scripts/check-compose.sh` using POSIX shell. Capture `docker compose config`, isolate the rendered `app:` block with `awk`, and require rendered `mem_limit` to equal 1073741824 bytes and `pids_limit` to equal 64. The script prints only static failure messages and exits nonzero if either value is absent or changed.

Run:

```bash
sh scripts/check-compose.sh
```

Expected: FAIL because the limits are not present yet.

- [ ] **Step 5: Add Compose limits and item-limit environment propagation**

Under the `app` service add:

```yaml
mem_limit: 1g
pids_limit: 64
```

Ensure `OMDI_MAX_JOB_ITEMS:` is in the app environment from Task 1. Do not apply the runtime memory limit to the `tools` builder service.

- [ ] **Step 6: Build the collection service with a fixed fake-only Compose override**

Create `docker/compose.collection.yaml`:

```yaml
services:
  app:
    command:
      - sh
      - -ec
      - >-
        mkdir -p "$$GOTMPDIR";
        go build -tags=omdi_testextractor -trimpath -o /home/omdi/.cache/go-build/omdi-dev ./cmd/omdi;
        exec /home/omdi/.cache/go-build/omdi-dev serve
```

Add `COLLECTION_COMPOSE := $(COMPOSE) -f compose.yaml -f docker/compose.collection.yaml` and use it consistently for `up`, `exec`, `run`, and `down` inside `collection-test`. Normal `compose-up`, `build`, `docker-build`, and runtime image builds remain untagged and real. Do not expose a general build-tags environment variable.

- [ ] **Step 7: Add extractor and Compose gates to CI**

Change:

```sh
full_packages=${COVERAGE_FULL_PACKAGES:-internal/auth internal/storage internal/urlpolicy internal/extractor}
```

Add a `compose-check` Make target that runs `sh scripts/check-compose.sh`, add it to `.PHONY`, and invoke it in `ci` before image build. Keep `coverage` responsible for the statement gates.

- [ ] **Step 8: Run contract, build-selection, and configuration verification**

Run:

```bash
make fmt
docker compose run --rm --no-deps tools go test ./internal/api ./cmd/omdi -count=1
docker compose run --rm --no-deps tools go test -tags=omdi_testextractor ./cmd/omdi -count=1
make compose-check
make coverage
```

Expected: OpenAPI-related Go tests pass, both build modes pass, Compose limits validate, and all four security-critical packages report 100%.

- [ ] **Step 9: Run the offline Bruno collection**

Run:

```bash
make collection-test
```

Expected: the fake-tagged service produces one synthetic MP4; no request to Vimeo or any other public host occurs.

- [ ] **Step 10: Commit synchronized contracts and gates when explicitly authorized**

```bash
git add internal/api/jobs_test.go docs/openapi.yaml collection/jobs compose.yaml docker/compose.collection.yaml Makefile scripts/check-coverage.sh scripts/check-compose.sh
git commit -m "test: synchronize real extractor contracts" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 11: Opt-In Real Extraction Smoke Test

**Files:**
- Create: `scripts/real-smoke.sh`
- Create: `scripts/test-real-smoke.sh`
- Modify: `Makefile`
- Modify: `.gitignore` only if the script needs a new temporary-name pattern; prefer `/tmp` so no ignore change is needed.

**Interfaces:**
- Consumes: `OMDI_REAL_SMOKE_URL`, Docker Compose, production real composition, API-key CLI, job API, and operator purge/revoke commands.
- Produces: `make real-smoke URL=...`; it is never a dependency of `ci`.

- [ ] **Step 1: Add a failing shell-harness test for required input and cleanup**

Create `scripts/test-real-smoke.sh` that places a fake `docker` executable in a `mktemp -d` directory, prepends it to `PATH`, and records invocations. Cover:

1. no `OMDI_REAL_SMOKE_URL` exits 2 before invoking Docker;
2. a synthetic successful API helper path reaches purge, key revoke, and `compose down` cleanup;
3. an injected submission failure still reaches the same cleanup;
4. recorded output never contains the synthetic URL or API key.

Use a trap to remove the test directory.

- [ ] **Step 2: Run the shell test and observe the missing script**

Run:

```bash
sh scripts/test-real-smoke.sh
```

Expected: FAIL because `scripts/real-smoke.sh` does not exist.

- [ ] **Step 3: Implement trap-based Compose and credential cleanup**

Start `scripts/real-smoke.sh` with `set -eu`, require a nonempty `OMDI_REAL_SMOKE_URL`, and never enable shell tracing. Track the captured API key and owner ID in private shell variables. Install a trap before `compose up` that, when values exist, runs owner-scoped `omdi jobs purge --yes --owner`, revokes the temporary key, and always runs `docker compose down --remove-orphans`. Redirect cleanup diagnostics that could contain runtime details.

- [ ] **Step 4: Implement a non-echoing Python API probe inside the app container**

Create the temporary key with `docker compose exec -T app $(APP_BINARY) keys create --name real-smoke-<timestamp>` and pass the key and URL only as environment variables to `python3` through `docker compose exec -T -e ... app`. The embedded Python must:

```python
payload = json.dumps({"url": os.environ["OMDI_REAL_SMOKE_URL"]}).encode()
# POST /v1/jobs, poll Location until a terminal status, require succeeded,
# stream every download_url to /tmp with 64 KiB chunks, require nonzero size,
# unlink every temporary download, and print only "real smoke: ok".
```

Use `urllib.request`, a one-second poll interval, and a deadline equal to the configured job timeout plus 30 seconds. On HTTP or job failure, raise a static error that does not include response content, URL, token, or path.

- [ ] **Step 5: Add the injection-safe Make target**

```make
real-smoke: export OMDI_REAL_SMOKE_URL := $(URL)
real-smoke:
	@test -n "$$OMDI_REAL_SMOKE_URL" || { echo "usage: make real-smoke URL=<public-post-url>" >&2; exit 2; }
	@sh scripts/real-smoke.sh
```

Add `real-smoke` to `.PHONY` but not to `ci`. Export through Make's environment mechanism; never interpolate `$(URL)` into a shell command or generated JSON.

- [ ] **Step 6: Run syntax, harness, and missing-input tests without network access**

Run:

```bash
sh -n scripts/real-smoke.sh scripts/test-real-smoke.sh
sh scripts/test-real-smoke.sh
make real-smoke
```

Expected: syntax and harness pass; `make real-smoke` exits 2 with only the static usage line. Do not run with a real URL in required verification.

- [ ] **Step 7: Commit the opt-in smoke workflow when explicitly authorized**

```bash
git add scripts/real-smoke.sh scripts/test-real-smoke.sh Makefile
git commit -m "test: add opt-in real extraction smoke" -m "Co-Authored-By: codex <codex@openai.com>"
```

### Task 12: Documentation, Phase Closure, and Complete Verification

**Files:**
- Modify: `README.md`
- Modify: `SECURITY.md`
- Modify: `docs/architecture.md`
- Modify: `docs/roadmap.md`
- Modify: `.env.example`
- Create: `docs/superpowers/records/2026-09-23-phase-3-real-extractors.md`

**Interfaces:**
- Consumes: all delivered Phase 3 behavior and verification evidence.
- Produces: accurate operator/security documentation and Phase 3 `Complete` status.

- [ ] **Step 1: Update README behavior and configuration**

Replace the fake-extractor limitation with the real routing table, one-post rule, 20-item default, compatibility-first formats, and no-transcoding rule. Document that required collection tests use the fake-tagged binary, production/default builds use real extraction, and `make real-smoke URL=...` is optional and performs real network access. Add `OMDI_MAX_JOB_ITEMS` and `real-smoke` to the existing tables without adding a batch API example. Confirm `.env.example` contains exactly one commented `# OMDI_MAX_JOB_ITEMS=20` entry.

- [ ] **Step 2: Expand the security policy with extractor controls**

Document the trusted pinned-binary boundary and the hostile URL/output boundary; loopback proxy validation on every connection; public-address and rebinding checks; port, byte, item, time, diagnostic, memory, PID, and disk limits; local-only ffprobe/FFmpeg; process-group cancellation; and the prohibition on cookies, credentials, private media, DRM bypass, and raw diagnostics.

- [ ] **Step 3: Update architecture from planned to implemented**

Describe the production `extractor.Real`, static platform routing, job-scoped egress session, subprocess runner, inspection/remux flow, build-tagged collection fake, and unchanged worker/storage/API boundaries. Remove statements saying Phase 3 has not started or the service always produces synthetic media.

- [ ] **Step 4: Run formatting and the full Go test suite**

Run:

```bash
make fmt
make fmt-check
make test
```

Expected: formatting clean and race-enabled tests pass.

- [ ] **Step 5: Run coverage, lint, vulnerability, and secret checks**

Run:

```bash
make coverage
make lint
make vuln
make secret-scan
```

Expected: total coverage at least 90%, all four critical packages at 100%, and no lint, known vulnerability, or secret findings.

- [ ] **Step 6: Run builds, Compose validation, and offline executable contracts**

Run:

```bash
make build
make compose-check
make docker-build
make smoke
make image-scan
make collection-test
sh scripts/test-real-smoke.sh
```

Expected: all commands pass; the required checks do not perform a real media download.

- [ ] **Step 7: Run the aggregate gate**

Run:

```bash
make ci
```

Expected: PASS. If Docker is unavailable, record exactly which Docker-backed commands could not run; do not claim complete verification.

- [ ] **Step 8: Write the Phase 3 delivery record from observed evidence**

Create `docs/superpowers/records/2026-09-23-phase-3-real-extractors.md` only after verification. Record delivered scope, the design and plan links, exact commands run and their outcomes, coverage percentages, security invariants, and any explicitly deferred work. Do not copy the implementation plan or include URLs, keys, tokens, paths, diagnostics, or real media details.

- [ ] **Step 9: Mark Phase 3 complete from verified evidence**

Change the roadmap current position to Phase 3 complete and Phase 4 next, set Phase 3 `Status: Complete`, link the Phase 3 design and the delivery record created in Step 8, and leave Phase 4 `Planned`. Do not make this change when any required verification in Steps 4–7 failed or could not run.

- [ ] **Step 10: Commit documentation and phase closure when explicitly authorized**

```bash
git add README.md SECURITY.md docs/architecture.md docs/roadmap.md docs/superpowers/records/2026-09-23-phase-3-real-extractors.md .env.example
git commit -m "docs: complete Phase 3 real extractors" -m "Co-Authored-By: codex <codex@openai.com>"
```

- [ ] **Step 11: Confirm the final worktree state without pushing**

Run:

```bash
git status --short --branch
git log --oneline --decorate -15
```

Expected: no uncommitted Phase 3 files, the authorized atomic commits are visible, and nothing has been pushed.
