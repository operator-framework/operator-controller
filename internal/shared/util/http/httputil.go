package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// ImmediateFallbackDialContext creates a DialContext function that tries connection
// attempts sequentially in the order returned by DNS, without the 300ms Happy Eyeballs
// delay. This respects DNS server ordering while eliminating the racing delay.
//
// Go's standard Happy Eyeballs implementation (RFC 6555/8305) is in the net package:
// https://cs.opensource.google/go/go/+/refs/tags/go1.25.3:src/net/dial.go;l=525 (DialContext)
// https://cs.opensource.google/go/go/+/refs/tags/go1.25.3:src/net/dial.go;l=585 (dialParallel)
func ImmediateFallbackDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Split the address into host and port
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	klog.InfoS("Resolving DNS for connection", "host", host, "port", port, "network", network)

	// Resolve all IP addresses for the host
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		klog.ErrorS(err, "DNS resolution failed", "host", host)
		return nil, err
	}

	if len(ips) == 0 {
		err := fmt.Errorf("no IP addresses found for host %s", host)
		klog.ErrorS(err, "DNS resolution returned no addresses", "host", host)
		return nil, err
	}

	// Convert IPs to strings for logging
	ipStrings := make([]string, 0, len(ips))
	for _, ip := range ips {
		ipStrings = append(ipStrings, ip.String())
	}
	klog.InfoS("DNS resolution complete", "host", host, "addressCount", len(ips), "addresses", ipStrings)

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Try each address sequentially in the order DNS returned them
	var lastErr error
	for i, ip := range ips {
		// Determine address type and dial network
		var addrType, dialNetwork string
		if ip.To4() != nil {
			addrType = "IPv4"
			dialNetwork = network
			if network == "tcp" {
				dialNetwork = "tcp4"
			}
		} else {
			addrType = "IPv6"
			dialNetwork = network
			if network == "tcp" {
				dialNetwork = "tcp6"
			}
		}

		target := net.JoinHostPort(ip.String(), port)
		klog.InfoS("Attempting connection", "host", host, "type", addrType,
			"address", ip.String(), "port", port, "attempt", i+1, "of", len(ips))

		conn, err := dialer.DialContext(ctx, dialNetwork, target)
		if err == nil {
			klog.InfoS("Successfully connected", "host", host, "type", addrType,
				"address", ip.String(), "port", port)
			return conn, nil
		}
		klog.ErrorS(err, "Connection failed", "host", host, "type", addrType,
			"address", ip.String(), "port", port, "attempt", i+1, "of", len(ips))
		lastErr = err
	}

	klog.ErrorS(lastErr, "All connection attempts failed", "host", host, "totalAttempts", len(ips))
	return nil, lastErr
}

// ConfigureDefaultTransport configures http.DefaultTransport to use ImmediateFallbackDialContext.
// This affects all HTTP clients that use the default transport, including the containers/image
// library used for pulling from registries. Returns an error if DefaultTransport is not *http.Transport.
func ConfigureDefaultTransport() error {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("http.DefaultTransport is not *http.Transport, cannot configure custom dialer")
	}
	transport.DialContext = ImmediateFallbackDialContext
	return nil
}

func BuildHTTPClient(cpw *CertPoolWatcher) (*http.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	pool, _, err := cpw.Get()
	if err != nil {
		return nil, err
	}

	// Clone the default transport to inherit custom dialer and other defaults
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("http.DefaultTransport is not *http.Transport, cannot build HTTP client")
	}
	tlsTransport := transport.Clone()
	tlsTransport.TLSClientConfig = &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}
	httpClient.Transport = tlsTransport

	return httpClient, nil
}
