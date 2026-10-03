# Reddit Media and Share Links Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking. Execution method remains subject to user selection.

**Goal:** Download native Reddit images, galleries, and videos from direct post URLs and subreddit share links.

**Architecture:** Resolve share redirects in the worker through the existing egress session and validate each destination with the configured URL policy. Route Reddit through the pinned yt-dlp anonymous session for validated native images, ordered galleries, and compatible video/audio, as amended by the approved correction below. Reuse inspection, storage, item tokens, and persisted bundles.

**Tech Stack:** Go standard library, existing SQLite repositories, Docker Compose/Makefile, gallery-dl, yt-dlp, FFmpeg/ffprobe, OpenAPI, and Bruno.

**Spec:** [Approved design](../specs/2026-10-02-reddit-media-and-share-links-design.md), commit `5dd6270`.

## Execution Status

Approved and committed as `10b3fce`; native execution selected. On 2026-10-02, the user explicitly removed Bruno collection changes from scope. Task 3 Step 5 is skipped, and Step 7 verifies the existing collection through `make ci`; Go API and real-tool tests cover the new behavior. Product commits remain subject to separate authorization.

## Global Constraints

- No behavior changes before this plan is approved and committed with explicit authorization and an execution method is selected.
- No user-provided links, subreddit names, share codes, job IDs, or log identifiers in committed artifacts. Use synthetic fixtures only.
- All code, documentation, comments, API descriptions, and messages in English.
- Use Makefile targets through `rtk`; do not require host Go or media tools.
- Apply TDD to behavior changes; maintain at least 90% unit coverage and 100% for all packages listed by `scripts/check-coverage.sh`.
- Share codes contain exactly ten ASCII alphanumeric characters. Preserve the submitted normalized URL in storage and API responses.
- At most five redirect responses, a 30-second overall resolution timeout, and 64 KiB response headers; all resolution and download traffic shares one proxy session and transfer budget.
- Reuse existing tool dependencies; if a capability requires an update, use a verified newest stable pinned release and update its lock/artifact definitions together.
- No new HTTP route, response field, database migration, credential support, external-link recursion, or thumbnail substitution.
- Every authorized commit is conventional and atomic and ends with exactly one `Co-Authored-By: codex <codex@openai.com>` trailer. No push authorization exists.

## Review Focus

- Tracking differences can hide redirect cycles: detect cycles using normalized URLs, with a hop bound as a second guard. Test in Task 1.
- An accepted Location can resolve to private addresses: prove the actual production proxy rejects that hop. Test in Task 1.
- Embedded yt-dlp options can silently bypass the proxy or drop audio: check propagated options and exercise synthetic DASH streams. Test in Task 2.
- A tool can skip oversized files and still exit successfully: reject partial output and retain resource-limit outcomes. Test in Task 2.
- Offline fake fixtures can falsely imply redirect coverage: keep HTTP collection assertions separate from real resolver and adapter tests. Test/document in Task 3.

## File Map

- `internal/urlpolicy/post.go`, `normalize_test.go`: recognize valid Reddit share paths.
- New `internal/extractor/reddit.go`, `reddit_test.go`: proxy-bound HTTP resolution and fixed failures.
- `internal/extractor/gallerydl.go`, new `reddit_gallery_test.go`, existing gallery tests: restricted Reddit media options and validated output.
- `internal/extractor/real.go`, `real_test.go`, `phase3_coverage_test.go`: resolver composition and Reddit routing.
- `cmd/omdi/main.go`, `extractor_real.go`, `extractor_fake.go`, affected CLI tests: pass the configured policy to real composition.
- `internal/api/jobs_test.go`, new `reddit_test.go`: URL acceptance, preserved URL, image and gallery HTTP flows.
- `internal/extractor/extractor.go`, `fake_test.go`: deterministic synthetic Reddit fixtures used only by existing fake composition.
- New `collection/reddit/` requests: offline image/share/gallery workflows.
- `docs/openapi.yaml`, `README.md`, `docs/architecture.md`, `docs/roadmap.md`: document and track delivered behavior.

## Task 1: Validated Reddit Share Resolution

**Interfaces:**

- Consumes `(*urlpolicy.Policy).Normalize(string) (urlpolicy.Result, error)`.
- Produces `resolveRedditURL(ctx context.Context, raw, proxyURL string, policy *urlpolicy.Policy) (string, error)` in `internal/extractor/reddit.go`.
- Its private testable core is `resolveRedditRedirects(ctx context.Context, raw string, policy *urlpolicy.Policy, client *http.Client) (string, error)`; tests can supply a synthetic RoundTripper. Production constructs the transport explicitly, without a new exported interface.
- Direct post/gallery URLs and bare `redd.it` post links pass through unchanged after validation; only `/r/.../s/...` triggers resolution. Intermediate `redd.it` URLs are resolved until a direct post/gallery Location is reached.

- [x] **Step 1: Add normalization regression tests.** Extend `internal/urlpolicy/normalize_test.go` with the following expectation and rejected codes of lengths 9/11, punctuation, escaped separators, and trailing path components:

  ```go
  got, err := policy.Normalize("https://www.reddit.com/r/example/s/Ab12Cd34Ef/?utm_source=example#fragment")
  if err != nil || got.Platform != "reddit" || got.URL != "https://www.reddit.com/r/example/s/Ab12Cd34Ef/" {
      t.Fatalf("Normalize = %#v, %v", got, err)
  }
  ```

- [x] **Step 2: Run `rtk make test`.** Confirm the valid-share regression fails with unsupported URL before changing normalization.
- [x] **Step 3: Extend `normalizeReddit` in `internal/urlpolicy/post.go`.** Recognize the exact share pattern, retaining all current rejection and query-stripping rules.
- [x] **Step 4: Add resolver tests in `internal/extractor/reddit_test.go`.** Use named table tests for 301/302/303/307/308, relative Location, normalized direct/gallery destination, intermediate short/share links, exactly five allowed responses and rejection of a sixth, normalized cycles, non-redirect share pages, absent/invalid Location, unsafe schemes/credentials/ports, other platforms, unsupported paths, and too-long URLs. Assert request counts: invalid destinations are never contacted and final post pages are not fetched. Use synthetic subreddit `example`, share code `Ab12Cd34Ef`, and post ID `abc123`.
- [x] **Step 5: Add resolver transport/failure tests.** Assert explicit loopback proxy selection even with unrelated proxy environment variables; no Authorization header or cookie jar; 403/404/429 map to the specified fixed details; other unsuccessful statuses and malformed chains map to `tool_error`; transport/header failures and timeout map to `network_error`; parent cancellation returns its context error; all response bodies close. Inspect the configured 30-second timeout and 64 KiB header limit without a 30-second test delay. Exercise timeout using a short test-client deadline.
- [x] **Step 6: Run `rtk make test`.** Confirm resolver tests fail because the resolver is missing.
- [x] **Step 7: Implement the resolver and HTTP transport.** Validate proxy and starting URL, use manual GET redirects, normalize each Location before issuing another request, count responses, close bodies without reading, close idle connections, and return pathless `Failure` values. Preserve cancellation and allow `Real.Extract` to attach egress statistics and resource-limit precedence.
- [x] **Step 8: Add a production-proxy integration test in `reddit_test.go`.** Reuse the existing proxy test resolver/dialer patterns and a synthetic TLS endpoint. Allow one public-address fixture hop then make the next approved Reddit hostname resolve to a private address; assert proxy rejection and zero dial attempts to the private destination. Exercise session-budget exhaustion and shared-session closure as well. Do not disable TLS verification in production to accommodate fixtures.
- [x] **Step 9: Run `rtk make test` and `rtk make coverage`.** Require passing tests and full coverage for changed security-critical code. Update roadmap follow-up status to `In progress` during execution.
- [x] **Step 10: Check `rtk git diff --check` and create an authorized atomic commit** for the URL policy and resolver, titled `fix(reddit): validate and resolve share redirects`, with the required trailer. If implementation commits have not been authorized, leave verified changes uncommitted.

## Task 2: Native Reddit Media and Production Routing

**Interfaces:**

- Consume Task 1's `resolveRedditURL`.
- Change composition to `NewReal(proxy *urlpolicy.Proxy, ytdlp *YTDLP, gallery *GalleryDL, policy *urlpolicy.Policy) *Real`.
- Add `resolveReddit func(context.Context, string, string) (string, error)` to `Real`, capturing the configured policy in `NewReal`. Production supplies it; tests use a concrete function seam.
- Change both build-tag variants of `newExtractorComponents` to `(cfg config.Config, policy *urlpolicy.Policy) (extractorComponents, error)` and pass the already-created policy from `serve`.
- Preserve `NewGalleryDL(path string, runner *Runner, media *MediaTools) *GalleryDL`; the same-package media dependency already holds the FFmpeg path. Add a focused `redditGalleryOptions(request Request, proxyURL, ffmpegPath string) []string` helper for Reddit-only configuration arguments.

- [x] **Step 1: Add failing Reddit gallery-adapter tests.** In new `internal/extractor/reddit_gallery_test.go`, assert public REST mode; zero comments, recursion and self-text links; previews disabled; empty child whitelist; `videos="dash"`; embedded `yt_dlp` module; shared proxy; existing `ytdlpFormat`; configured FFmpeg path; MP4 merge; three retries; 30-second sockets; one concurrent fragment; `MaxBytes` filesize limits; no metadata, subtitles, comments, thumbnails or remote components; filenames `item-{num:03}.{extension}`; range `1-(MaxItems+1)`. Parse structured JSON option values when checking embedded yt-dlp options, rather than comparing serialization order.
- [x] **Step 2: Add output/failure tests.** Synthetic tool/probe fixtures produce a JPEG, ordered two-image gallery, and H.264/AAC MP4. Assert media types, ordering and sound-stream inspection. Reject empty output with `Failure{Detail: job.DetailNoMedia, Tool: "gallery-dl"}`, excess items and oversized skips as `ErrTooLarge`, tool failure after partial output, symlinks, gaps, unsafe names, and incompatible codecs. Preserve X argument and empty-output behavior.
- [x] **Step 3: Run `rtk make test` and confirm the Reddit adapter rejects requests before implementation.**
- [x] **Step 4: Implement the Reddit options and adapter support.** Extend request platform validation; append options only for Reddit. Reuse discovery and `MediaTools.Finalize`. Distinguish empty valid Reddit extraction from malformed output; preserve existing X outcomes. Recognize the embedded downloader's oversized-file indication as well as gallery-dl's existing indication, even when it exits zero.
- [x] **Step 5: Exercise real installed tools with synthetic media.** Add a bounded container-backed fixture under `tests/` for gallery-dl's actual Reddit parser/downloader against synthetic metadata, JPEGs and DASH video/audio. Inject fixture HTTP responses only in the test process, preserving and asserting production proxy settings. Generate small synthetic streams with the installed FFmpeg; verify merged MP4 contains H.264 video and AAC audio. Test external child links are ignored and no files exceed configured names/range. Wire invocation through a Makefile target and `make ci`; never use real user media as a fixture. This catches tool integration behavior a fake executable cannot prove.
- [x] **Step 6: Add failing `Real` routing tests.** Reddit resolves and then invokes gallery-dl exactly once within the same session. Direct/share requests leave the caller's original Request unchanged. Failed/canceled resolution does not invoke either adapter and still closes the session. Budget failures and egress failures keep existing precedence; all other platforms keep their existing routes. Adjust constructor coverage tests for the new policy dependency.
- [x] **Step 7: Run `rtk make test` and confirm routing regressions fail before changing `Real`.**
- [x] **Step 8: Implement routing and composition.** Call the resolver inside the acquired session, modify only the local request copy, and route Reddit to gallery-dl. Update both tagged composition functions and all callers/tests. Leave yt-dlp's existing internal Reddit capability intact unless removal is necessary; no unrelated cleanup.
- [x] **Step 9: Run `rtk make test`, `rtk make coverage`, and the new synthetic tool integration target.** Require preserved X behavior and full changed-package coverage.
- [x] **Step 10: Check `rtk git diff --check` and create an authorized atomic commit**, titled `fix(reddit): download native post images and galleries`, with the required trailer; otherwise leave changes uncommitted.

## Task 3: HTTP Contract, Offline Collection, and Documentation

**Interfaces:** Use existing job, item-token and bundle response schemas; add no fields or routes.

- [x] **Step 1: Add failing API tests.** In new `internal/api/reddit_test.go`, queue direct and shared synthetic posts through `newAPIFixture`, assert `202`, `platform=reddit`, tracking removal, stored/returned original share URL, and no network access during POST. Reject malformed share input with safe JSON `422 unsupported_url`. Complete synthetic image/gallery jobs using existing storage/store helpers; assert JPEG download headers and bytes, ordered gallery items, bundle presence, and ZIP entries matching those items. Include polling from another owner and URL-free logs.
- [x] **Step 2: Add fake-fixture tests in `internal/extractor/fake_test.go`.** Reserve direct `/comments/abc123/` and share `/r/example/s/Ab12Cd34Ef/` for the same one-image payload, and `/gallery/def456/` for a two-image payload. Assert identical direct/share media, limits and cancellation. These synthetic selectors apply to existing fake composition only; production has no fixture branch.
- [x] **Step 3: Run `rtk make test` and confirm fixture/API expectations fail before changing Fake.**
- [x] **Step 4: Extend Fake fixtures in `internal/extractor/extractor.go`.** Preserve existing synthetic and Instagram behavior. Use synthetic non-personal payloads and existing private file permissions.
- [x] **Step 5: Skipped by explicit user direction; keep the existing collection unchanged. Originally planned: Add `collection/reddit/folder.yml` and sequential requests.** Name requests `jobs-reddit-share`, `get-job-reddit-share`, `get-download-reddit-image`, `jobs-reddit-gallery`, `get-job-reddit-gallery`, and `get-bundle-reddit-gallery`, plus `jobs-reddit-invalid-share`. Use distinct `redditImageJobId`, `redditImageToken`, `redditGalleryJobId`, and `redditBundleToken` variables. Follow existing bounded polling before dependent downloads, asserting JPEG/ZIP types, nonzero size, privacy headers, tracking removal, and preserved share URL. Download calls send no API key. Keep fixture values in requests or existing collection variables; modify environments only if new environment settings are needed.
- [x] **Step 6: Update `docs/openapi.yaml` and platform documentation.** Describe direct/share acceptance and native image/gallery/video support, normalized source URL persistence, worker-only redirect resolution and current access limitations. Preserve all response schemas/statuses. Update README platform examples, architecture routing/security boundaries, and the roadmap follow-up entry. Keep synthetic examples only; the new accepted URL form needs no unrelated release/version change.
- [x] **Step 7: Run `rtk make test` and `rtk make collection-test`.** Require every new collection scenario to pass offline; restore the local app with `rtk make compose-up-detached` if it was running. Explain explicitly in collection docs that fake share scenarios do not test real redirection.
- [x] **Step 8: Check `rtk git diff --check` and create an authorized atomic commit**, titled `test(reddit): cover media and share link HTTP workflows`, with the required trailer; otherwise leave verified changes uncommitted.

## Task 4: Final Verification and Handoff

- [x] **Step 1: Review the complete diff against the approved specification.** Check destination validation, shared transfer limits, embedded downloader settings, source URL persistence, gallery ordering, unchanged X routing, and credential/log privacy. Follow the selected execution method's review requirements.
- [x] **Step 2: Run `rtk make ci`.** Require all formatting, race tests, coverage, lint, security, build, image, workflow, Compose, synthetic tool integration, smoke and offline collection checks to pass. Restore the service if it was running.
- [x] **Step 3: Run live verification with the existing `make real-smoke` target.** Supply the user's direct and share URLs transiently through the target's runtime environment; do not write them into repository files, commit messages, scripts or reports. Verify the same JPEG result for both forms and native video audio using a public video fixture. Use only temporary private runtime files and remove them; report external access/rate limits as unverified live cases rather than success. Restore the service after smoke targets stop it.
- [x] **Step 4: Mark the roadmap follow-up complete only after required checks pass.** Record verification outcomes without personal identifiers and update plan checkboxes. Do not claim live download success unless it was observed.
- [x] **Step 5: Run final privacy and whitespace checks.** Inspect the whole change for user-provided links/IDs, secrets, retained real media, runtime data, and unrelated artifacts. Check every created commit's required trailer appears exactly once. Create a final documentation commit only if authorized.
- [x] **Step 6: Hand off with behavior, test evidence and material limitations.** Link changed files and mention commits created. Do not push without explicit authorization.

## Execution Handoff

This plan was approved and committed, and native execution was selected.
The original recommendation was native execution: the resolver,
adapter and HTTP tasks share a small number of sequential interfaces, so one
implementer can complete them with less coordination. Apply the selected
workflow's independent review before final handoff.

## Verification Outcome

The complete `make ci` suite passed, including 96.8% overall unit coverage, 100% coverage for required security packages, synthetic real-tool tests, and the unchanged Bruno collection (14 requests, 9 tests, 61 assertions). Independent review findings were reproduced with failing regressions and corrected; closure review found no remaining actionable issues. Live direct/share/video extraction attempts were blocked by Reddit network security on the verification network, so live media success is not claimed. Product changes were copied to the main checkout without staging or commits, as explicitly requested.

## Video Compatibility Correction

A subsequent live comparison in the same container showed gallery-dl REST access blocked while the original yt-dlp Reddit extractor recognized the native video's formats. The interim correction kept gallery-dl as the primary native-media adapter and retried with the pinned yt-dlp module only after a blocked result, before any output exists, with an active context, no session error, and no rejected egress destination. Reuse the resolved request and proxy session so the original URL, transfer budget, and inspection policy remain unchanged. Restrict yt-dlp to the Reddit extractor and abort on missing fragments; external post links remain outside scope. Regression tests cover successful retry, propagated failure, cancellation, transfer limits, egress denial, partial output, missing output directory, and non-blocked failures. Live API download verified an MP4 with H.264 video and AAC audio; no user URL or media was retained in project artifacts. Changes remain uncommitted on main by user instruction.

The interim native-only retry inspected the first unprocessed extraction result before downloading. URL-transparent and playlist results return no_media without following their children; native crossposts remain supported. The fixed helper runs with isolated Python in the existing media-tool virtual environment, receives JSON options and the URL as separate arguments, disables plugin directories, and reuses bounded runner output and media inspection. Real-tool synthetic tests cover native video, native crosspost, and linked-post rejection.

The video compatibility correction passed the complete `make ci` suite on main, with 96.9% unit coverage and 100% for required security packages. The original Bruno collection remains unchanged and passes all 14 requests, 9 tests, and 61 assertions. No implementation commit or push was created.


## Approved anonymous native-media correction

The user approved using the working yt-dlp anonymous Reddit session for native images and galleries as well as videos. Public REST blocking must no longer gate native extraction. Resolve share links in the existing session, then use one native-media adapter with no recursive URL/playlist processing. Capture the post metadata obtained by the pinned Reddit extractor; download only original native image URLs or ordered gallery entries, through the same proxy and byte/item limits. Preserve native video format selection and H.264/AAC inspection. Reject previews, external links, incomplete galleries, and unsafe metadata. Access errors must remain access errors, rather than being replaced by a video-only no_media result.

Execution completed inline on main without changes to the Bruno collection. The user subsequently validated the behavior and authorized final commits. Write failing pinned-tool photo/gallery and routing regressions first; implement the bounded helper; verify negative fixtures, full coverage and CI; verify live image and video downloads transiently; restore the development service.


### Native-media execution evidence

- Pinned-tool photo and gallery regressions first failed with no_media; the native helper now downloads original JPEGs and preserves gallery order, including preview-host original source conversion.
- Routing regressions first demonstrated the unwanted REST dependency; Reddit now invokes the native yt-dlp adapter once after resolution in the original egress session.
- Private-file regressions first failed for image permissions; originals now use exclusive creation with mode 0600.
- Processing-video and traceback privacy regressions first failed; known native processing media returns unavailable, and fixed exception handling suppresses tracebacks that could repeat post identifiers.
- Final review identified silent progressive-video size rejection under quiet mode. A pinned-tool oversized progressive MP4 regression first reproduced the wrong error; retaining bounded tool output preserves the max-filesize marker and returns too_large.
- Pinned-tool cases verify native video/audio, native crosspost video, missing fragments, linked-post rejection, preview rejection, invalid original URLs, incomplete/failed/truncated galleries, and item/byte limits. Live API verification downloaded a JPEG via a share URL and an H.264/AAC MP4; no submitted URL, post identifier, or real media was saved in project artifacts.
- The final complete `make ci` passed with 96.9% unit coverage, 100% coverage for required security packages, all pinned-tool regressions, lint/security/build/image/smoke checks, and the unchanged Bruno collection (14 requests, 9 tests, 61 assertions). The user validated the delivered behavior and authorized final commits on main; live gallery verification is not claimed.
