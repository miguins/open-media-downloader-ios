# Reddit Media and Share Links Design

## Status and Intended Outcome

Initially approved by the user on 2026-10-02. The user approved the subsequent
yt-dlp native-media correction on 2026-10-03. Implementation and user validation
completed on main; the user subsequently authorized final commits. The Bruno
collection remains unchanged. Phases 0–5 remain complete; this is a focused
follow-up fix.

A user should be able to submit a public Reddit post using either its direct URL
or the mobile share URL and download the post's supported media. The reported
failure involves a public post whose media is a JPEG on `i.redd.it`: yt-dlp
recognizes the post but delegates to the image URL, for which no enabled
extractor exists. Separately, URL normalization rejects `/r/<subreddit>/s/<code>`
before a worker can resolve its redirect.

Scope includes native Reddit images, image galleries, and native videos. Gallery
support is an extension of the requested image support using the existing item
and bundle model. Preserve source order, existing supported media types, native
video audio, resource limits, and operation without user credentials.

## Alternatives and Decision

The initial approved design routed Reddit through gallery-dl. Live comparison showed its public REST access blocked on the verification network while yt-dlp's anonymous Reddit session could read the same post. The user subsequently approved the yt-dlp direction for images and galleries, preserving working native video behavior.

Use the pinned yt-dlp Reddit extractor's anonymous session to obtain post metadata and download native media directly. This avoids repeated REST requests and failure-based routing. Capture metadata in a concrete Reddit extractor subclass, inspect the first unprocessed result, and never follow URL-transparent or playlist children. Resolve share redirects explicitly in Go before extraction, preserving the application's final post validation boundary. X remains on gallery-dl.

## URL Acceptance and Redirect Resolution

Extend `internal/urlpolicy/post.go` to accept HTTPS Reddit subreddit share paths
`/r/<subreddit>/s/<code>` with an optional trailing slash. Use the existing
subreddit character rules and exactly ten ASCII alphanumeric characters for
the share code, matching the installed gallery-dl share pattern. Strip tracking
parameters and fragments through existing normalization. Existing direct post,
gallery, and `redd.it/<post-id>` URLs remain accepted. User profiles, subreddit
feeds, comment permalinks, malformed share paths, and submitted direct media
URLs remain outside this change.

Keep API normalization local and free of network access. POST queues the
normalized submitted share URL just as it queues a direct post. The persisted
and returned job URL remains that submitted normalized URL; resolution changes
only the worker's extraction request. No migration or new response field is
needed.

Add a focused Reddit share resolver in `internal/extractor`. Run it only for
share paths and after `Real.Extract` acquires its egress session. Use a Go HTTP
transport configured explicitly with the session proxy, TLS verification,
bounded response headers, and the job context. Do not use environment proxy
selection or direct-network fallback.

Resolve redirects manually with GET requests and automatic redirects disabled.
Recognize 301, 302, 303, 307, and 308. Permit at most five redirect responses,
with a 30-second overall resolution timeout subordinate to the job deadline
and at most 64 KiB of response headers per response. Close every response body
without reading page contents. Relative Location values are resolved against
the current request URL.

Validate each Location with the existing URL policy before making any next
request. It must normalize to platform `reddit` and identify either another
allowed share/short URL or a direct post/gallery URL. Reject credentials,
non-HTTPS URLs, non-default ports, unsupported paths, other platforms, and
malformed locations. The proxy continues to validate DNS and connected public
addresses at every hop. On reaching a direct post/gallery URL, stop resolving
and pass its normalized value to the native-media adapter without an extra page request.

Reject missing Location, redirect loops, exhausted hop counts, and a final
non-redirect share response; do not treat page HTML as proof of a valid post.
Cancellation, expiry, proxy errors, and the shared transfer budget apply to
resolution and media extraction together.

## Reddit Media Extraction

Route Reddit through `YTDLP.ExtractRedditMedia` after share resolution, keeping other platform routes unchanged. Run a fixed helper with isolated Python from the same pinned virtual environment as the configured yt-dlp executable. Pass JSON options and the resolved URL as separate subprocess arguments; disable plugin directories and external components.

A concrete Reddit extractor subclass captures the post JSON fetched by upstream's anonymous session. Extract without downloading or processing child results. Native video formats retain the existing compatibility selection, pinned FFmpeg stream-copy merge, bounded retries, and missing-fragment rejection. Native crosspost videos remain supported by upstream's metadata handling. Do not introduce credentials or cookie imports.

A native single image must resolve directly to an HTTPS `i.redd.it` original with a safe supported filename. Galleries use `gallery_data.items` order and valid `media_metadata` original sources. Preview-host gallery source URLs are converted to their original `i.redd.it` URLs; standalone preview URLs and thumbnail substitution remain excluded. Validate all gallery sources and the item count before creating files. Reject incomplete or malformed galleries rather than silently dropping entries.

Stream image downloads through the same downloader/proxy session into exclusive `0600` fixed-name files. Enforce cumulative image bytes alongside the session-wide transfer budget, and abort on truncated bodies or failed requests. Reuse safe numbered discovery, media inspection, storage ingestion, and bundle creation. A failed adapter returns no publishable files. Genuine absence of native media returns `no_media`; access errors retain their existing failure classification.

## Errors, Privacy, and HTTP Contract

Use existing error codes and fixed `error_detail` values. Resolver HTTP 403,
404, and 429 map to `forbidden`, `unavailable`, and `rate_limited`. Timeout and
transport failures map to `network_error`, with existing egress statistics
taking precedence where applicable. Invalid redirect behavior maps to
`tool_error`; resource exhaustion retains existing resource-limit behavior.
Cancellation retains the worker's current cancellation semantics.

No URL, share code, Location value, cookie, internal path, or raw tool diagnostic
is added to logs or client errors. Existing sanitized debug diagnostics remain
available for tool failures. Redirect requests carry no API authorization header.

Update `docs/openapi.yaml` descriptions and examples for accepted Reddit share
URLs and supported native media. Add organized Bruno scenarios named
`jobs-reddit-share`, `get-job-reddit-share`, and related image/gallery download
checks, using synthetic fixtures through the existing private fake build tag.
The offline collection proves API acceptance, polling, item downloads, and
multi-item bundle behavior; it does not claim to exercise real redirects.
Document that returned job URLs preserve the submitted normalized share form.

Update README and architecture platform descriptions. Track the follow-up in
the roadmap when implementation begins and update its status only after
verification. Do not mark this specification as delivered behavior.

## Verification and Acceptance

Use TDD beside each affected package. Maintain at least 90% unit coverage and
100% for `internal/urlpolicy`, `internal/extractor`, and the other existing
security-critical packages.

Required cases:

- Share normalization accepts valid mixed-case codes and strips tracking;
  rejects bad lengths, escaped separators, nested paths, and unsupported routes.
- Resolver handles relative locations, direct post/gallery destinations,
  intermediate allowed Reddit URLs, and short URLs within the hop bound.
- Resolver rejects loops, excess hops, missing/malformed locations, unexpected
  responses, foreign hosts/platforms, credentials, downgrade URLs, private
  addresses, invalid ports, and unsupported final paths before contacting them.
- Resolver obeys cancellation, timeout, header bounds, response closure, and
  the shared egress budget. Unit tests use synthetic HTTP/proxy fixtures.
- Adapter routing and arguments preserve X behavior and enforce Reddit's
  restrictions, proxy propagation to yt-dlp, format preference, FFmpeg path,
  safe filenames, and bounded limits.
- Single images, ordered galleries, native video with audio, empty output,
  oversized files, excess items, incomplete extraction, unsafe outputs, and
  incompatible formats follow their specified outcomes.
- API tests and Bruno cover direct/share URL acceptance, returned URL semantics,
  image polling/download, and gallery bundle behavior without live Reddit.

Run the appropriate Makefile checks and complete `make ci` when Docker is
available. Restore the development service if verification stops it. After
offline checks pass, run the existing real-smoke target for the user-provided
public image post using both its direct and share URLs, and for a public native
video. Inspect JPEG/media type and video streams in temporary runtime storage;
retain no real media in the repository. Report external rate limits or access
blocks separately from verified local behavior.

## Delivery Gates

Review and approve this written specification, and explicitly authorize its
atomic conventional commit. Then write, review, approve, and authorize the
commit of a current-format implementation plan and select its execution method.
Only then change product behavior. Every authorized commit must carry the
repository's required Codex co-author trailer exactly once.

## References

- [gallery-dl Reddit implementation](https://github.com/mikf/gallery-dl/blob/master/gallery_dl/extractor/reddit.py)
- [gallery-dl configuration](https://github.com/mikf/gallery-dl/blob/master/docs/configuration.rst)
- [gallery-dl yt-dlp downloader](https://github.com/mikf/gallery-dl/blob/master/gallery_dl/downloader/ytdl.py)
