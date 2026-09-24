// Package urlpolicy normalizes submitted media URLs and enforces network destination policy.
package urlpolicy

import "net/url"

// platform is a supported media platform and the registrable domains it serves.
type platform struct {
	id        string
	domains   []string
	normalize func(*url.URL) error
}

func supportedPlatforms() []platform {
	return []platform{
		{id: "youtube", domains: []string{"youtube.com", "youtu.be"}, normalize: normalizeYouTube},
		{id: "instagram", domains: []string{"instagram.com"}, normalize: normalizeInstagram},
		{id: "tiktok", domains: []string{"tiktok.com"}, normalize: normalizeTikTok},
		{id: "x", domains: []string{"x.com", "twitter.com"}, normalize: normalizeX},
		{id: "reddit", domains: []string{"reddit.com", "redd.it"}, normalize: normalizeReddit},
		{id: "vimeo", domains: []string{"vimeo.com"}, normalize: normalizeVimeo},
	}
}

// PlatformIDs returns the identifiers of every supported platform in a stable order.
func PlatformIDs() []string {
	platforms := supportedPlatforms()
	ids := make([]string, 0, len(platforms))
	for _, p := range platforms {
		ids = append(ids, p.id)
	}

	return ids
}

// IsPlatform reports whether id identifies a supported platform.
func IsPlatform(id string) bool {
	for _, p := range supportedPlatforms() {
		if p.id == id {
			return true
		}
	}

	return false
}
