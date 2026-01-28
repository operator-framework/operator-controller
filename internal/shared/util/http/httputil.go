package http

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// IPv4PreferringDialContext creates a DialContext function that prefers IPv4 addresses
// when both IPv4 and IPv6 addresses are available. It tries all IPv4 addresses first,
// and only falls back to IPv6 if all IPv4 connection attempts fail. If only one type
// is available, it uses whichever is present.
func IPv4PreferringDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Split the address into host and port
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	klog.V(4).InfoS("Resolving DNS for connection", "host", host, "port", port, "network", network)

	// Resolve all IP addresses for the host
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		klog.V(2).ErrorS(err, "DNS resolution failed", "host", host)
		return nil, err
	}

	// Separate IPv4 and IPv6 addresses
	var ipv4Addrs, ipv6Addrs []net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			ipv4Addrs = append(ipv4Addrs, ip)
		} else {
			ipv6Addrs = append(ipv6Addrs, ip)
		}
	}

	klog.V(4).InfoS("DNS resolution complete", "host", host, "ipv4Count", len(ipv4Addrs), "ipv6Count", len(ipv6Addrs))
	if len(ipv4Addrs) > 0 {
		klog.V(4).InfoS("IPv4 addresses found", "host", host, "addresses", ipv4Addrs)
	}
	if len(ipv6Addrs) > 0 {
		klog.V(4).InfoS("IPv6 addresses found", "host", host, "addresses", ipv6Addrs)
	}

	// Try to connect to each address, preferring IPv4 first
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	// Try all IPv4 addresses first
	var lastErr error
	for i, ip := range ipv4Addrs {
		dialNetwork := network
		if network == "tcp" {
			dialNetwork = "tcp4"
		}

		target := net.JoinHostPort(ip.String(), port)
		klog.V(2).InfoS("Attempting IPv4 connection", "host", host, "address", ip.String(), "port", port, "attempt", i+1, "of", len(ipv4Addrs))

		conn, err := dialer.DialContext(ctx, dialNetwork, target)
		if err == nil {
			klog.InfoS("Successfully connected via IPv4", "host", host, "address", ip.String(), "port", port)
			return conn, nil
		}
		klog.V(2).ErrorS(err, "IPv4 connection failed", "host", host, "address", ip.String(), "port", port, "attempt", i+1, "of", len(ipv4Addrs))
		lastErr = err
	}

	// If all IPv4 attempts failed, try IPv6 addresses
	if len(ipv4Addrs) > 0 && len(ipv6Addrs) > 0 {
		klog.V(2).InfoS("All IPv4 attempts failed, falling back to IPv6", "host", host, "ipv4Tried", len(ipv4Addrs))
	}

	for i, ip := range ipv6Addrs {
		dialNetwork := network
		if network == "tcp" {
			dialNetwork = "tcp6"
		}

		target := net.JoinHostPort(ip.String(), port)
		klog.V(2).InfoS("Attempting IPv6 connection", "host", host, "address", ip.String(), "port", port, "attempt", i+1, "of", len(ipv6Addrs))

		conn, err := dialer.DialContext(ctx, dialNetwork, target)
		if err == nil {
			klog.InfoS("Successfully connected via IPv6", "host", host, "address", ip.String(), "port", port)
			return conn, nil
		}
		klog.V(2).ErrorS(err, "IPv6 connection failed", "host", host, "address", ip.String(), "port", port, "attempt", i+1, "of", len(ipv6Addrs))
		lastErr = err
	}

	klog.ErrorS(lastErr, "All connection attempts failed", "host", host, "ipv4Tried", len(ipv4Addrs), "ipv6Tried", len(ipv6Addrs))
	return nil, lastErr
}

func BuildHTTPClient(cpw *CertPoolWatcher) (*http.Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}

	pool, _, err := cpw.Get()
	if err != nil {
		return nil, err
	}

	// Clone the default transport to inherit IPv4 preference and other defaults
	tlsTransport := http.DefaultTransport.(*http.Transport).Clone()
	tlsTransport.TLSClientConfig = &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}
	httpClient.Transport = tlsTransport

	return httpClient, nil
}
