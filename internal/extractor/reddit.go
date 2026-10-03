package extractor

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

// resolveRedditURL resolves only share links, within the caller's egress session.
func resolveRedditURL(ctx context.Context, raw, proxyURL string, policy *urlpolicy.Policy) (string, error) {
	if !validProxyURL(proxyURL) {
		return "", redditFailure(job.DetailToolError)
	}
	proxy, _ := url.Parse(proxyURL) // validProxyURL has parsed it.
	transport := &http.Transport{
		Proxy:                  http.ProxyURL(proxy),
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	return resolveRedditRedirects(ctx, raw, policy, client)
}

func resolveRedditRedirects(ctx context.Context, raw string, policy *urlpolicy.Policy, client *http.Client) (string, error) {
	normalized, err := policy.Normalize(raw)
	if err != nil || normalized.Platform != "reddit" {
		return "", redditFailure(job.DetailToolError)
	}
	if !redditShareURL(normalized.URL) {
		return canonicalRedditURL(normalized.URL), nil
	}
	timeout := 30 * time.Second
	if client.Timeout > 0 {
		timeout = client.Timeout
	}
	resolutionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	current := normalized.URL
	seen := make(map[string]bool)
	for range 5 {
		if seen[current] {
			return "", redditFailure(job.DetailToolError)
		}
		seen[current] = true
		request, _ := http.NewRequestWithContext(resolutionCtx, http.MethodGet, current, nil) // Normalize validated it.
		// RoundTrip never parses or follows a Location before our validation.
		response, err := client.Transport.RoundTrip(request)
		if err != nil {
			if ctx.Err() == context.Canceled {
				return "", ctx.Err()
			}
			return "", redditFailure(job.DetailNetworkError)
		}
		_ = response.Body.Close()
		switch response.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		case http.StatusForbidden:
			return "", redditFailure(job.DetailForbidden)
		case http.StatusNotFound:
			return "", redditFailure(job.DetailUnavailable)
		case http.StatusTooManyRequests:
			return "", redditFailure(job.DetailRateLimited)
		default:
			return "", redditFailure(job.DetailToolError)
		}
		location := response.Header.Get("Location")
		if location == "" {
			return "", redditFailure(job.DetailToolError)
		}
		next, err := request.URL.Parse(location)
		if err != nil {
			return "", redditFailure(job.DetailToolError)
		}
		normalized, err = policy.Normalize(next.String())
		if err != nil || normalized.Platform != "reddit" {
			return "", redditFailure(job.DetailToolError)
		}
		current = normalized.URL
		if !strings.EqualFold(next.Hostname(), "redd.it") && !redditShareURL(current) {
			return canonicalRedditURL(current), nil
		}
	}
	return "", redditFailure(job.DetailToolError)
}

func redditFailure(detail job.ErrorDetail) *Failure { return &Failure{Detail: detail, Tool: "reddit"} }

// Both helpers receive URLs already accepted by the configured URL policy.
func redditShareURL(raw string) bool {
	parsed, _ := url.Parse(raw)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return len(parts) == 4 && parts[0] == "r" && parts[2] == "s"
}

func canonicalRedditURL(raw string) string {
	parsed, _ := url.Parse(raw)
	parts := strings.Split(parsed.Path, "/")
	index := 2
	if parsed.Hostname() == "redd.it" {
		index = 1
	} else if parts[1] == "r" {
		index = 4
	}
	parts[index] = strings.ToLower(parts[index])
	parsed.Path = strings.Join(parts, "/")
	return parsed.String()
}
