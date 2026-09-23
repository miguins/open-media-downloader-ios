package urlpolicy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"
)

const resolveTimeout = 5 * time.Second

// ErrNonPublicAddress reports a destination that is not a public unicast address.
var ErrNonPublicAddress = errors.New("destination is not a public address")

// globalUnicastIPv6 is the only IPv6 range considered for public destinations.
var globalUnicastIPv6 = netip.MustParsePrefix("2000::/3")

// nonPublicPrefixes are special-purpose ranges not covered by netip.Addr predicates.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this" network
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space (CGNAT)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("192.88.99.0/24"),  // 6to4 relay anycast
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved and limited broadcast
	netip.MustParsePrefix("2001::/23"),       // IETF protocol assignments, including Teredo and benchmarking
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4
	netip.MustParsePrefix("3fff::/20"),       // documentation
}

// IsPublic reports whether addr is a globally routable unicast address. IPv4-mapped IPv6
// addresses are judged by their IPv4 form; zoned addresses and IPv6 outside 2000::/3 are
// never public.
func IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.Zone() != "" || !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	if addr.Is6() && !globalUnicastIPv6.Contains(addr) {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}

	return true
}

// Resolver looks up the IP addresses of a host.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// CheckResolved resolves host and fails unless every returned address is public.
func CheckResolved(ctx context.Context, resolver Resolver, host string) error {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("%w: host did not resolve", ErrNonPublicAddress)
	}
	for _, addr := range addrs {
		if !IsPublic(addr) {
			return ErrNonPublicAddress
		}
	}

	return nil
}

// DialControl is a net.Dialer Control function that refuses connections to non-public
// addresses. It runs after name resolution, so it also defeats DNS rebinding.
func DialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ErrNonPublicAddress
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !IsPublic(addr) {
		return ErrNonPublicAddress
	}

	return nil
}
