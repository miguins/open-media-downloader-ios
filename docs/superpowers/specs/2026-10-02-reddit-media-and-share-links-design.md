# Reddit Media and Share Links Design

## Status and Intended Outcome

Approved by the user on 2026-10-02, with authorization to commit this
specification. Product behavior changes remain subject to implementation-plan
approval and execution-method selection. Phases 0–5 remain complete; this is a
focused follow-up fix.

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

1. **Route Reddit through gallery-dl.** Reuse its native post-media extraction
   and the existing gallery adapter, including yt-dlp integration for native
   video manifests. This is recommended because one extraction path covers
   images, galleries, and videos.
2. **Keep yt-dlp first and fall back to gallery-dl.** This preserves the current
   video path but introduces repeated requests, partial-output cleanup, and
   fragile failure-based routing for ordinary image posts.
3. **Implement Reddit JSON extraction in Go.** This offers explicit media
   selection but duplicates upstream session, crosspost, gallery, and video
   handling. It is unnecessary for this fix.

Resolve share redirects explicitly in Go before invoking gallery-dl. Its built-in
share extractor queues another extractor, which conflicts with the adapter's
empty child-extractor whitelist and does not provide the application's final
post validation boundary.

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
and pass its normalized value to gallery-dl without an extra page request.

Reject missing Location, redirect loops, exhausted hop counts, and a final
non-redirect share response; do not treat page HTML as proof of a valid post.
Cancellation, expiry, proxy errors, and the shared transfer budget apply to
resolution and media extraction together.

## Reddit Media Extraction

Change `internal/extractor/real.go` to route Reddit through `GalleryDL` after
share resolution; keep other platform routes as currently implemented.
Extend `GalleryDL` request validation to allow Reddit and X. Keep Reddit-specific
options separate from X options.

Configure Reddit explicitly for public REST access, zero comments, zero
recursion, no self-text links, and no preview fallback. Retain
`extractor.whitelist=[]` to prevent child extractors from following external
post links. Native media in crossposts may be handled by gallery-dl's existing
post-media logic. Do not introduce credentials, cookie imports, or a generic
external-link downloader.

Use Reddit's DASH video mode and the yt-dlp downloader module already installed
with the application. Set its proxy explicitly to the same session, use the
existing `ytdlpFormat` preference, configure the existing FFmpeg path, and merge
compatible audio/video into MP4 without transcoding. Bound retries, socket
timeouts, fragments, file sizes, and output diagnostics consistently with the
existing adapters. Disable remote components, ancillary metadata downloads,
and extra output files. Use gallery-dl's generated `item-NNN.extension` names
for both images and merged videos. Any adapter constructor change is wired
through the existing app composition and its tests.

Keep the existing `MaxItems + 1` detection, byte limits, safe output discovery,
media inspection, storage ingestion, and bundle creation. Native images are
delivered as media files, never replaced by thumbnails. Unsupported media
codecs/types fail existing validation. Empty output produces `no_media` rather
than success; no partial gallery is marked succeeded after an extraction error.

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
