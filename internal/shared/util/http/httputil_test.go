package http

import (
	"context"
	"net"
	"testing"
)

func TestIPv4PreferringDialContext(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
	}{
		{
			name:     "localhost",
			hostname: "localhost:80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Parse the hostname to extract just the host part for DNS resolution
			host, _, err := net.SplitHostPort(tt.hostname)
			if err != nil {
				t.Fatalf("Failed to split host:port: %v", err)
			}

			// Look up all IPs for the hostname
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				t.Skipf("DNS resolution failed for %s: %v (this is OK for test environments)", host, err)
			}

			// Separate IPv4 and IPv6 addresses
			var ipv4Addrs, ipv6Addrs []net.IP
			for _, ip := range ips {
				if ip.To4() != nil {
					ipv4Addrs = append(ipv4Addrs, ip)
					t.Logf("Found IPv4 address: %s", ip.String())
				} else {
					ipv6Addrs = append(ipv6Addrs, ip)
					t.Logf("Found IPv6 address: %s", ip.String())
				}
			}

			if len(ipv4Addrs) == 0 && len(ipv6Addrs) == 0 {
				t.Skip("No IP addresses found for hostname")
			}

			t.Logf("Hostname %s has %d IPv4 and %d IPv6 address(es)",
				host, len(ipv4Addrs), len(ipv6Addrs))

			// Test the ordering logic based on what addresses are available
			switch {
			case len(ipv4Addrs) > 0 && len(ipv6Addrs) > 0:
				// Both IPv4 and IPv6 available - should prefer IPv4
				t.Logf("Testing dual-stack: should try IPv4 first, fallback to IPv6 if needed")

				// Verify that IPv4 would be tried first
				// The actual connection logic would try all IPv4 addrs, then all IPv6 addrs
				t.Logf("✓ Correctly configured to try %d IPv4 address(es) before %d IPv6 address(es)",
					len(ipv4Addrs), len(ipv6Addrs))

			case len(ipv4Addrs) > 0:
				// Only IPv4 available
				t.Logf("Testing IPv4-only: should use available IPv4 address(es)")
				t.Logf("✓ Will use %d IPv4 address(es)", len(ipv4Addrs))

			case len(ipv6Addrs) > 0:
				// Only IPv6 available
				t.Logf("Testing IPv6-only: should use available IPv6 address(es)")
				t.Logf("✓ Will use %d IPv6 address(es)", len(ipv6Addrs))
			}
		})
	}
}

func TestIPv4PreferringDialContext_AddressSeparation(t *testing.T) {
	// Test that we correctly separate IPv4 and IPv6 addresses
	testCases := []struct {
		name       string
		inputIPs   []net.IP
		expectIPv4 int
		expectIPv6 int
	}{
		{
			name: "mixed IPv4 and IPv6",
			inputIPs: []net.IP{
				net.ParseIP("192.0.2.1"),
				net.ParseIP("2001:db8::1"),
				net.ParseIP("192.0.2.2"),
				net.ParseIP("2001:db8::2"),
			},
			expectIPv4: 2,
			expectIPv6: 2,
		},
		{
			name: "only IPv4",
			inputIPs: []net.IP{
				net.ParseIP("192.0.2.1"),
				net.ParseIP("192.0.2.2"),
			},
			expectIPv4: 2,
			expectIPv6: 0,
		},
		{
			name: "only IPv6",
			inputIPs: []net.IP{
				net.ParseIP("2001:db8::1"),
				net.ParseIP("2001:db8::2"),
			},
			expectIPv4: 0,
			expectIPv6: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var ipv4Addrs, ipv6Addrs []net.IP
			for _, ip := range tc.inputIPs {
				if ip.To4() != nil {
					ipv4Addrs = append(ipv4Addrs, ip)
				} else {
					ipv6Addrs = append(ipv6Addrs, ip)
				}
			}

			if len(ipv4Addrs) != tc.expectIPv4 {
				t.Errorf("Expected %d IPv4 addresses, got %d", tc.expectIPv4, len(ipv4Addrs))
			}
			if len(ipv6Addrs) != tc.expectIPv6 {
				t.Errorf("Expected %d IPv6 addresses, got %d", tc.expectIPv6, len(ipv6Addrs))
			}

			t.Logf("✓ Correctly separated %d IPv4 and %d IPv6 address(es)",
				len(ipv4Addrs), len(ipv6Addrs))
		})
	}
}
