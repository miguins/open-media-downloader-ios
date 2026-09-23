// Package urlpolicy normalizes submitted media URLs and enforces network destination policy.
package urlpolicy

// platform is a supported media platform and the registrable domains it serves.
type platform struct {
	id      string
	domains []string
}

func supportedPlatforms() []platform {
	return []platform{
		{id: "youtube", domains: []string{"youtube.com", "youtu.be"}},
		{id: "instagram", domains: []string{"instagram.com"}},
		{id: "tiktok", domains: []string{"tiktok.com"}},
		{id: "x", domains: []string{"x.com", "twitter.com"}},
		{id: "reddit", domains: []string{"reddit.com", "redd.it"}},
		{id: "vimeo", domains: []string{"vimeo.com"}},
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
