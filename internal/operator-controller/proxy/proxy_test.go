package proxy

import (
	"os"
	"testing"
)

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "valid URL without credentials",
			input:    "http://proxy.example.com:8080",
			expected: "http://proxy.example.com:8080",
		},
		{
			name:     "valid URL with credentials",
			input:    "http://user:pass@proxy.example.com:8080",
			expected: "http://proxy.example.com:8080",
		},
		{
			name:     "hostname without credentials",
			input:    "proxy.example.com:8080",
			expected: "proxy.example.com:8080",
		},
		{
			name:     "hostname with credentials (unparseable URL)",
			input:    "user:pass@proxy.example.com:8080",
			expected: "<redacted>",
		},
		{
			name:     "invalid format with @ but no credentials",
			input:    "something@weird",
			expected: "<redacted>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNewFromEnv(t *testing.T) {
	// Save original env vars
	origHTTP := os.Getenv("HTTP_PROXY")
	origHTTPS := os.Getenv("HTTPS_PROXY")
	origNO := os.Getenv("NO_PROXY")

	// Restore at end
	defer func() {
		os.Setenv("HTTP_PROXY", origHTTP)
		os.Setenv("HTTPS_PROXY", origHTTPS)
		os.Setenv("NO_PROXY", origNO)
	}()

	tests := []struct {
		name       string
		httpProxy  string
		httpsProxy string
		noProxy    string
		expectNil  bool
		expectHash bool
	}{
		{
			name:       "all empty returns nil",
			httpProxy:  "",
			httpsProxy: "",
			noProxy:    "",
			expectNil:  true,
		},
		{
			name:       "http proxy only",
			httpProxy:  "http://proxy.example.com:8080",
			httpsProxy: "",
			noProxy:    "",
			expectNil:  false,
			expectHash: true,
		},
		{
			name:       "all proxies set",
			httpProxy:  "http://proxy.example.com:8080",
			httpsProxy: "https://proxy.example.com:8443",
			noProxy:    "localhost,.cluster.local",
			expectNil:  false,
			expectHash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("HTTP_PROXY", tt.httpProxy)
			os.Setenv("HTTPS_PROXY", tt.httpsProxy)
			os.Setenv("NO_PROXY", tt.noProxy)

			p := NewFromEnv()

			if tt.expectNil {
				if p != nil {
					t.Errorf("NewFromEnv() = %v, want nil", p)
				}
				return
			}

			if p == nil {
				t.Fatal("NewFromEnv() = nil, want non-nil")
			}

			if p.HTTPProxy != tt.httpProxy {
				t.Errorf("HTTPProxy = %q, want %q", p.HTTPProxy, tt.httpProxy)
			}
			if p.HTTPSProxy != tt.httpsProxy {
				t.Errorf("HTTPSProxy = %q, want %q", p.HTTPSProxy, tt.httpsProxy)
			}
			if p.NoProxy != tt.noProxy {
				t.Errorf("NoProxy = %q, want %q", p.NoProxy, tt.noProxy)
			}

			if tt.expectHash {
				fingerprint := p.Fingerprint()
				if fingerprint == "" {
					t.Error("Fingerprint() = empty string, want non-empty hash")
				}
				// Verify fingerprint is cached (calling again returns same value)
				if p.Fingerprint() != fingerprint {
					t.Error("Fingerprint() not cached properly")
				}
			}
		})
	}
}
