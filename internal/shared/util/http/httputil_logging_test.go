package http

import (
	"context"
	"flag"
	"testing"

	"k8s.io/klog/v2"
)

// TestIPv4PreferringDialContext_WithLogging demonstrates the logging output
// Run with: go test -v -args -v=4 to see all logs
func TestIPv4PreferringDialContext_WithLogging(t *testing.T) {
	// Initialize klog flags for testing
	klog.InitFlags(nil)
	if err := flag.Set("v", "4"); err != nil {
		t.Fatalf("Failed to set v flag: %v", err)
	}
	if err := flag.Set("logtostderr", "true"); err != nil {
		t.Fatalf("Failed to set logtostderr flag: %v", err)
	}

	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "dual-stack hostname (localhost)",
			address: "localhost:80",
		},
		{
			name:    "IPv4 only hostname",
			address: "127.0.0.1:80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			t.Logf("Testing connection to %s (this will fail but demonstrate logging)", tt.address)

			// Attempt connection (will fail since nothing is listening on port 80)
			// but we'll see the logging output
			_, err := IPv4PreferringDialContext(ctx, "tcp", tt.address)

			// We expect connection refused or similar, not a DNS error
			if err != nil {
				t.Logf("Connection failed as expected: %v", err)
			}
		})
	}
}
