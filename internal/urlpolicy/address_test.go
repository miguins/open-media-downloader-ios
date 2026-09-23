package urlpolicy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestIsPublic(t *testing.T) {
	public := []string{
		"1.1.1.1", "8.8.8.8", "93.184.216.34", "100.63.255.255", "100.128.0.0", "172.32.0.1", "192.0.1.1",
		"198.20.0.1", "223.255.255.255", "2606:4700:4700::1111", "2a00:1450:4001:80b::200e", "2001:4860:4860::8888",
		"::ffff:8.8.8.8",
	}
	nonPublic := []string{
		"0.0.0.0", "0.1.2.3", "10.0.0.1", "100.64.0.1", "100.127.255.255", "127.0.0.1", "127.255.255.254",
		"169.254.169.254", "172.16.0.1", "172.31.255.255", "192.0.0.8", "192.0.2.1", "192.88.99.1",
		"192.168.1.1", "198.18.0.1", "198.19.255.255", "198.51.100.1", "203.0.113.1", "224.0.0.1",
		"239.255.255.250", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254", "::127.0.0.1", "64:ff9b::7f00:1",
		"64:ff9b:1::1", "100::1", "2001::1", "2001:2::1", "2001:db8::1", "2002:7f00:1::", "3fff::1",
		"5f00::1", "fc00::1", "fd12:3456::1", "fe80::1", "fec0::1", "ff02::1", "fe80::1%eth0",
	}
	for _, value := range public {
		if !IsPublic(netip.MustParseAddr(value)) {
			t.Errorf("IsPublic(%s) = false; want true", value)
		}
	}
	for _, value := range nonPublic {
		if IsPublic(netip.MustParseAddr(value)) {
			t.Errorf("IsPublic(%s) = true; want false", value)
		}
	}
	if IsPublic(netip.Addr{}) {
		t.Error("IsPublic(invalid) = true")
	}
}

type fakeResolver struct {
	addrs    []netip.Addr
	err      error
	deadline bool
}

func (f *fakeResolver) LookupNetIP(ctx context.Context, network, _ string) ([]netip.Addr, error) {
	_, f.deadline = ctx.Deadline()
	if network != "ip" {
		return nil, errors.New("unexpected network")
	}

	return f.addrs, f.err
}

func TestCheckResolved(t *testing.T) {
	tests := []struct {
		name     string
		resolver *fakeResolver
		wantErr  bool
	}{
		{name: "public", resolver: &fakeResolver{addrs: []netip.Addr{netip.MustParseAddr("8.8.8.8")}}},
		{name: "lookup failure", resolver: &fakeResolver{err: errors.New("no such host")}, wantErr: true},
		{name: "no addresses", resolver: &fakeResolver{}, wantErr: true},
		{name: "mixed", resolver: &fakeResolver{addrs: []netip.Addr{
			netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1"),
		}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckResolved(context.Background(), tt.resolver, "vimeo.com")
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckResolved() error = %v; wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrNonPublicAddress) {
				t.Fatalf("CheckResolved() error = %v; want ErrNonPublicAddress", err)
			}
			if !tt.resolver.deadline {
				t.Fatal("CheckResolved() did not bound the lookup")
			}
		})
	}
}

func TestDialControl(t *testing.T) {
	if err := DialControl("tcp4", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("DialControl(public) error = %v", err)
	}
	for _, address := range []string{"127.0.0.1:443", "[::1]:443", "[::ffff:10.0.0.1]:443", "not-an-address", "vimeo.com:443"} {
		if err := DialControl("tcp", address, nil); !errors.Is(err, ErrNonPublicAddress) {
			t.Fatalf("DialControl(%s) error = %v; want ErrNonPublicAddress", address, err)
		}
	}
}

func TestDialControlBlocksRebindingAtConnectTime(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	dialer := &net.Dialer{Timeout: time.Second, Control: DialControl}
	conn, err := dialer.DialContext(context.Background(), "tcp", listener.Addr().String())
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial to loopback succeeded")
	}
	if !errors.Is(err, ErrNonPublicAddress) {
		t.Fatalf("dial error = %v; want ErrNonPublicAddress", err)
	}
}
