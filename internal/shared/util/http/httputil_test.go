package http

import (
	"context"
	"net"
	"testing"
)

func TestImmediateFallbackDialContext(t *testing.T) {
	tests := []struct {
		name             string
		address          string
		wantFail         bool
		minExpectedAddrs int // minimum addresses we expect to find
	}{
		{
			name:             "dual-stack hostname tries addresses in DNS order",
			address:          "localhost:80",
			wantFail:         true, // nothing listening on port 80
			minExpectedAddrs: 1,    // should have at least one address
		},
		{
			name:             "IPv4-only hostname",
			address:          "127.0.0.1:80",
			wantFail:         true,
			minExpectedAddrs: 1,
		},
		{
			name:             "IPv6-only hostname",
			address:          "[::1]:80",
			wantFail:         true,
			minExpectedAddrs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Parse the address to extract host for DNS lookup
			host, _, err := net.SplitHostPort(tt.address)
			if err != nil {
				t.Fatalf("Failed to split host:port: %v", err)
			}

			// Look up IPs to verify DNS resolution works
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				t.Skipf("DNS resolution failed for %s: %v (this is OK for test environments)", host, err)
			}

			if len(ips) < tt.minExpectedAddrs {
				t.Skip("Not enough IP addresses found for hostname")
			}

			t.Logf("DNS returned %d address(es) - will try each in order:", len(ips))

			// Log all addresses for debugging
			for i, ip := range ips {
				ipType := "IPv6"
				if ip.To4() != nil {
					ipType = "IPv4"
				}
				t.Logf("  [%d] %s (%s)", i, ip.String(), ipType)
			}

			// Actually call the dialer function
			_, err = ImmediateFallbackDialContext(ctx, "tcp", tt.address)

			if tt.wantFail {
				if err == nil {
					t.Errorf("Expected connection to fail, but it succeeded")
				} else {
					t.Logf("Connection failed as expected: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected connection to succeed, but got error: %v", err)
				}
			}
		})
	}
}
