package urlpolicy

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

// ErrUnsupportedURL reports that a submitted URL violates the policy. Wrapped errors name
// the violated rule but never include the submitted value.
var ErrUnsupportedURL = errors.New("unsupported URL")

// hostProfile converts hosts to lowercase ASCII and rejects labels that are not valid
// letter-digit-hyphen DNS labels.
var hostProfile = idna.New(
	idna.MapForLookup(),
	idna.BidiRule(),
	idna.ValidateLabels(true),
	idna.VerifyDNSLength(true),
	idna.StrictDomainName(true),
)

// Policy accepts only public HTTPS URLs on allowed media platforms.
type Policy struct {
	maxLength int
	platforms []platform
}

// Result is a normalized URL and the platform it belongs to.
type Result struct {
	URL      string
	Platform string
}

// New returns a Policy allowing the given platform identifiers and URLs up to maxLength bytes.
func New(allowedPlatforms []string, maxLength int) (*Policy, error) {
	if maxLength <= 0 {
		return nil, errors.New("urlpolicy: maximum URL length must be positive")
	}
	if len(allowedPlatforms) == 0 {
		return nil, errors.New("urlpolicy: at least one platform must be allowed")
	}
	policy := &Policy{maxLength: maxLength}
	for _, allowed := range allowedPlatforms {
		if !IsPlatform(allowed) {
			return nil, errors.New("urlpolicy: unknown platform")
		}
		for _, p := range supportedPlatforms() {
			if p.id == allowed {
				policy.platforms = append(policy.platforms, p)
			}
		}
	}

	return policy, nil
}

// Normalize validates raw and returns its normalized form: lowercase ASCII host without
// a trailing dot or default port, and no fragment.
func (p *Policy) Normalize(raw string) (Result, error) {
	if raw == "" || len(raw) > p.maxLength {
		return Result{}, unsupported("length is out of range")
	}
	if !utf8.ValidString(raw) || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return Result{}, unsupported("contains invalid characters")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return Result{}, unsupported("is malformed")
	}
	if parsed.Scheme != "https" || parsed.Opaque != "" {
		return Result{}, unsupported("must use https")
	}
	if parsed.User != nil {
		return Result{}, unsupported("must not contain user information")
	}
	if port := parsed.Port(); (port != "" && port != "443") || strings.HasSuffix(parsed.Host, ":") {
		return Result{}, unsupported("must use the default port")
	}

	host, err := normalizeHost(parsed.Hostname())
	if err != nil {
		return Result{}, err
	}
	platformID, ok := p.match(host)
	if !ok {
		return Result{}, unsupported("host is not an allowed platform")
	}

	parsed.Scheme = "https"
	parsed.Host = host
	parsed.Fragment = ""
	parsed.RawFragment = ""

	return Result{URL: parsed.String(), Platform: platformID}, nil
}

func normalizeHost(host string) (string, error) {
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", unsupported("must contain a host")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return "", unsupported("must not use an IP address")
	}
	ascii, err := hostProfile.ToASCII(host)
	if err != nil {
		return "", unsupported("host is not a valid domain name")
	}

	return ascii, nil
}

func (p *Policy) match(host string) (string, bool) {
	for _, candidate := range p.platforms {
		for _, domain := range candidate.domains {
			if host == domain || strings.HasSuffix(host, "."+domain) {
				return candidate.id, true
			}
		}
	}

	return "", false
}

func unsupported(reason string) error {
	return fmt.Errorf("%w: URL %s", ErrUnsupportedURL, reason)
}
