package error

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeNetworkError(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "nil error returns empty string",
			err:      nil,
			expected: "",
		},
		{
			name:     "non-network error returns original message",
			err:      fmt.Errorf("some random error"),
			expected: "some random error",
		},
		{
			name: "strips read udp address pattern from DNS inner error",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &net.DNSError{
					IsNotFound:  false,
					IsTemporary: true,
					IsTimeout:   true,
					Server:      "10.96.0.10:53",
					Name:        "docker-registry.operator-controller.svc",
					Err:         "read udp 10.244.0.8:46753->10.96.0.10:53: i/o timeout",
				},
			},
			expected: "dial tcp: lookup docker-registry.operator-controller.svc on 10.96.0.10:53: i/o timeout",
		},
		{
			name: "strips read udp pattern from wrapped error preserving outer context",
			err: fmt.Errorf("source catalog content: error creating image source: %w", &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &net.DNSError{
					IsNotFound:  false,
					IsTemporary: true,
					IsTimeout:   true,
					Server:      "10.96.0.10:53",
					Name:        "docker-registry.operator-controller.svc",
					Err:         "read udp 10.244.0.8:46753->10.96.0.10:53: i/o timeout",
				},
			}),
			expected: "source catalog content: error creating image source: dial tcp: lookup docker-registry.operator-controller.svc on 10.96.0.10:53: i/o timeout",
		},
		{
			name: "net.DNSError with IsNotFound unchanged",
			err: &net.DNSError{
				IsNotFound: true,
				Server:     "10.96.0.10:53",
				Name:       "registry.example.com",
				Err:        "no such host",
			},
			expected: "lookup registry.example.com on 10.96.0.10:53: no such host",
		},
		{
			name: "net.DNSError without server unchanged",
			err: &net.DNSError{
				IsNotFound: true,
				Name:       "registry.example.com",
				Err:        "no such host",
			},
			expected: "lookup registry.example.com: no such host",
		},
		{
			name: "net.DNSError without read/write pattern unchanged",
			err: &net.DNSError{
				Server: "10.96.0.10:53",
				Name:   "registry.example.com",
				Err:    "server misbehaving",
			},
			expected: "lookup registry.example.com on 10.96.0.10:53: server misbehaving",
		},
		{
			name: "wrapped net.DNSError without read/write pattern preserves wrapping",
			err: fmt.Errorf("source catalog content: %w", &net.DNSError{
				Server: "10.96.0.10:53",
				Name:   "registry.example.com",
				Err:    "server misbehaving",
			}),
			expected: "source catalog content: lookup registry.example.com on 10.96.0.10:53: server misbehaving",
		},
		{
			name: "net.DNSError with both IsTimeout and IsNotFound unchanged",
			err: &net.DNSError{
				IsTimeout:  true,
				IsNotFound: true,
				Server:     "10.96.0.10:53",
				Name:       "registry.example.com",
				Err:        "i/o timeout",
			},
			expected: "lookup registry.example.com on 10.96.0.10:53: i/o timeout",
		},
		{
			name: "net.DNSError without server unchanged",
			err: &net.DNSError{
				IsTimeout: true,
				Name:      "registry.example.com",
				Err:       "i/o timeout",
			},
			expected: "lookup registry.example.com: i/o timeout",
		},
		{
			name: "net.OpError dial with source and addr unchanged",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Source: &net.TCPAddr{
					IP:   net.ParseIP("10.0.0.1"),
					Port: 52341,
				},
				Addr: &net.TCPAddr{
					IP:   net.ParseIP("192.168.1.100"),
					Port: 443,
				},
				Err: fmt.Errorf("connect: connection refused"),
			},
			expected: "dial tcp 10.0.0.1:52341->192.168.1.100:443: connect: connection refused",
		},
		{
			name: "net.OpError dial without source unchanged",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Addr: &net.TCPAddr{
					IP:   net.ParseIP("192.168.1.100"),
					Port: 443,
				},
				Err: fmt.Errorf("connect: connection refused"),
			},
			expected: "dial tcp 192.168.1.100:443: connect: connection refused",
		},
		{
			name: "wrapped net.OpError dial preserves full error string",
			err: fmt.Errorf("source catalog content: error creating image source: %w", &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Source: &net.TCPAddr{
					IP:   net.ParseIP("10.0.0.1"),
					Port: 52341,
				},
				Addr: &net.TCPAddr{
					IP:   net.ParseIP("192.168.1.100"),
					Port: 443,
				},
				Err: fmt.Errorf("connect: connection refused"),
			}),
			expected: "source catalog content: error creating image source: dial tcp 10.0.0.1:52341->192.168.1.100:443: connect: connection refused",
		},
		{
			name:     "strips read tcp pattern",
			err:      fmt.Errorf("read tcp 10.0.0.1:52341->192.168.1.100:443: connection reset by peer"),
			expected: "connection reset by peer",
		},
		{
			name:     "strips write tcp pattern",
			err:      fmt.Errorf("write tcp 10.0.0.1:52341->192.168.1.100:443: broken pipe"),
			expected: "broken pipe",
		},
		{
			name:     "strips read udp pattern with IPv6 addresses",
			err:      fmt.Errorf("read udp [::1]:52341->[fd00::1]:53: i/o timeout"),
			expected: "i/o timeout",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := SanitizeNetworkError(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSanitizeNetworkErrorConsistency(t *testing.T) {
	// Simulate DNS errors with different ephemeral ports in the read udp pattern
	makeError := func(sourcePort int) error {
		return &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{
				IsTemporary: true,
				IsTimeout:   true,
				Server:      "10.96.0.10:53",
				Name:        "docker-registry.operator-controller.svc",
				Err:         fmt.Sprintf("read udp 10.244.0.8:%d->10.96.0.10:53: i/o timeout", sourcePort),
			},
		}
	}

	// Different source ports should produce the same sanitized message
	msg1 := SanitizeNetworkError(makeError(46753))
	msg2 := SanitizeNetworkError(makeError(51234))
	msg3 := SanitizeNetworkError(makeError(60000))

	assert.Equal(t, msg1, msg2, "sanitized messages with different source ports should be equal")
	assert.Equal(t, msg2, msg3, "sanitized messages with different source ports should be equal")
	assert.Equal(t, "dial tcp: lookup docker-registry.operator-controller.svc on 10.96.0.10:53: i/o timeout", msg1)
}

func TestSanitizeNetworkErrorDNSConsistency(t *testing.T) {
	// Simulate DNS errors with different ephemeral ports wrapped in context
	makeError := func(sourcePort int) error {
		return fmt.Errorf("source catalog content: error creating image source: %w", &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{
				IsTemporary: true,
				IsTimeout:   true,
				Server:      "10.96.0.10:53",
				Name:        "registry.example.com",
				Err:         fmt.Sprintf("read udp 10.244.0.8:%d->10.96.0.10:53: i/o timeout", sourcePort),
			},
		})
	}

	msg1 := SanitizeNetworkError(makeError(46753))
	msg2 := SanitizeNetworkError(makeError(51234))
	msg3 := SanitizeNetworkError(makeError(60000))

	assert.Equal(t, msg1, msg2, "sanitized DNS messages with different source ports should be equal")
	assert.Equal(t, msg2, msg3, "sanitized DNS messages with different source ports should be equal")
	assert.Equal(t, "source catalog content: error creating image source: dial tcp: lookup registry.example.com on 10.96.0.10:53: i/o timeout", msg1)
}
