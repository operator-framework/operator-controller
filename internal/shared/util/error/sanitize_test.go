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
			name: "net.OpError wrapping net.DNSError",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &net.DNSError{
					IsNotFound:  false,
					IsTemporary: true,
					IsTimeout:   true,
					Server:      "10.96.0.10:53",
					Name:        "docker-registry.operator-controller.svc",
					Err:         "read udp 10.244.0.8:46753->10.96.0.10:53",
				},
			},
			expected: "lookup docker-registry.operator-controller.svc on 10.96.0.10:53: i/o timeout",
		},
		{
			name: "wrapped net.OpError wrapping net.DNSError",
			err: fmt.Errorf("source catalog content: error creating image source: %w", &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: &net.DNSError{
					IsNotFound:  false,
					IsTemporary: true,
					IsTimeout:   true,
					Server:      "10.96.0.10:53",
					Name:        "docker-registry.operator-controller.svc",
					Err:         "read udp 10.244.0.8:46753->10.96.0.10:53",
				},
			}),
			expected: "lookup docker-registry.operator-controller.svc on 10.96.0.10:53: i/o timeout",
		},
		{
			name: "net.DNSError without timeout",
			err: &net.DNSError{
				IsNotFound: true,
				Server:     "10.96.0.10:53",
				Name:       "registry.example.com",
				Err:        "no such host",
			},
			expected: "lookup registry.example.com on 10.96.0.10:53",
		},
		{
			name: "net.DNSError without server",
			err: &net.DNSError{
				IsTimeout: true,
				Name:      "registry.example.com",
				Err:       "i/o timeout",
			},
			expected: "lookup registry.example.com: i/o timeout",
		},
		{
			name: "net.OpError with source and addr strips source and wrapping",
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
			expected: "dial tcp: connect: connection refused",
		},
		{
			name: "net.OpError without source returns reconstructed message",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Addr: &net.TCPAddr{
					IP:   net.ParseIP("192.168.1.100"),
					Port: 443,
				},
				Err: fmt.Errorf("connect: connection refused"),
			},
			expected: "dial tcp: connect: connection refused",
		},
		{
			name: "wrapped net.OpError returns reconstructed message without wrapping",
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
			expected: "dial tcp: connect: connection refused",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := SanitizeNetworkError(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestSanitizeNetworkErrorConsistency(t *testing.T) {
	// Simulate the same error with different source ports (as happens between retries)
	makeError := func(sourcePort int) error {
		return fmt.Errorf("source catalog content: error creating image source: %w", &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Source: &net.TCPAddr{
				IP:   net.ParseIP("10.0.0.1"),
				Port: sourcePort,
			},
			Addr: &net.TCPAddr{
				IP:   net.ParseIP("192.168.1.100"),
				Port: 443,
			},
			Err: fmt.Errorf("connect: connection refused"),
		})
	}

	// Different source ports should produce the same sanitized message
	msg1 := SanitizeNetworkError(makeError(52341))
	msg2 := SanitizeNetworkError(makeError(52342))
	msg3 := SanitizeNetworkError(makeError(60000))

	assert.Equal(t, msg1, msg2, "sanitized messages with different source ports should be equal")
	assert.Equal(t, msg2, msg3, "sanitized messages with different source ports should be equal")
	assert.Equal(t, "dial tcp: connect: connection refused", msg1)
}

func TestSanitizeNetworkErrorDNSConsistency(t *testing.T) {
	// Simulate DNS errors with different ephemeral ports in the inner error string
	makeError := func(sourcePort int) error {
		return fmt.Errorf("source catalog content: error creating image source: %w", &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &net.DNSError{
				IsTemporary: true,
				IsTimeout:   true,
				Server:      "10.96.0.10:53",
				Name:        "registry.example.com",
				Err:         fmt.Sprintf("read udp 10.244.0.8:%d->10.96.0.10:53", sourcePort),
			},
		})
	}

	msg1 := SanitizeNetworkError(makeError(46753))
	msg2 := SanitizeNetworkError(makeError(51234))
	msg3 := SanitizeNetworkError(makeError(60000))

	assert.Equal(t, msg1, msg2, "sanitized DNS messages with different source ports should be equal")
	assert.Equal(t, msg2, msg3, "sanitized DNS messages with different source ports should be equal")
	assert.Equal(t, "lookup registry.example.com on 10.96.0.10:53: i/o timeout", msg1)
}
