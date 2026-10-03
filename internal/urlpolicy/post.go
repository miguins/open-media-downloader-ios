package urlpolicy

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	videoID       = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	youTubeShort  = regexp.MustCompile(`^/[A-Za-z0-9_-]+/?$`)
	youTubeVideo  = regexp.MustCompile(`^/(shorts|live)/[A-Za-z0-9_-]+/?$`)
	youTubeWatch  = regexp.MustCompile(`^/watch/?$`)
	instagramPost = regexp.MustCompile(`^/(?:[A-Za-z0-9_][A-Za-z0-9._]{0,29}/)?((?:p|reel|reels|tv)/[A-Za-z0-9_-]+/?)$`)
	tikTokShort   = regexp.MustCompile(`^/[A-Za-z0-9]+/?$`)
	tikTokPost    = regexp.MustCompile(`^/(@[A-Za-z0-9_.]+/video/[0-9]+|t/[A-Za-z0-9]+)/?$`)
	xPost         = regexp.MustCompile(`^/[A-Za-z0-9_]+/status/[0-9]+/?$`)
	redditShort   = regexp.MustCompile(`^/[A-Za-z0-9]+/?$`)
	redditPost    = regexp.MustCompile(`^/((r/[A-Za-z0-9_]+/)?comments/[A-Za-z0-9]+(/[A-Za-z0-9_-]+)?|gallery/[A-Za-z0-9]+|r/[A-Za-z0-9_]+/s/[A-Za-z0-9]{10})/?$`)
	vimeoPost     = regexp.MustCompile(`^/[0-9]+/?$`)
	vimeoPlayer   = regexp.MustCompile(`^/video/[0-9]+/?$`)
)

func normalizeYouTube(u *url.URL) error {
	if u.Host == "youtu.be" || strings.HasSuffix(u.Host, ".youtu.be") {
		return normalizePost(u, youTubeShort)
	}
	if !youTubeWatch.MatchString(u.Path) {
		return normalizePost(u, youTubeVideo)
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["v"]) != 1 || !videoID.MatchString(query.Get("v")) {
		return unsupported("must identify exactly one video")
	}
	if err := normalizePost(u, youTubeWatch); err != nil {
		return err
	}
	u.RawQuery = "v=" + url.QueryEscape(query.Get("v"))

	return nil
}

// normalizeInstagram also accepts the shared-link form that starts with the account
// handle and removes the handle, so both forms store the same URL.
func normalizeInstagram(u *url.URL) error {
	if err := normalizePost(u, instagramPost); err != nil {
		return err
	}
	u.Path = "/" + instagramPost.FindStringSubmatch(u.Path)[1]

	return nil
}

func normalizeTikTok(u *url.URL) error {
	if u.Host == "vm.tiktok.com" || u.Host == "vt.tiktok.com" {
		return normalizePost(u, tikTokShort)
	}

	return normalizePost(u, tikTokPost)
}

func normalizeX(u *url.URL) error {
	return normalizePost(u, xPost)
}

func normalizeReddit(u *url.URL) error {
	if u.Host == "redd.it" {
		return normalizePost(u, redditShort)
	}
	if strings.HasSuffix(u.Host, ".redd.it") {
		return unsupported("must use a post URL")
	}

	return normalizePost(u, redditPost)
}

func normalizeVimeo(u *url.URL) error {
	if u.Host == "player.vimeo.com" {
		return normalizePost(u, vimeoPlayer)
	}

	return normalizePost(u, vimeoPost)
}

// normalizePost validates the decoded path while rejecting escaped separators that
// would turn a different resource into an accepted path. The ASCII path patterns
// also exclude backslashes, dot segments, and nested percent escapes.
func normalizePost(u *url.URL, path *regexp.Regexp) error {
	if strings.Contains(strings.ToLower(u.RawPath), "%2f") || !path.MatchString(u.Path) {
		return unsupported("must use a direct-post path")
	}
	u.RawPath = ""
	u.RawQuery = ""
	u.ForceQuery = false

	return nil
}
