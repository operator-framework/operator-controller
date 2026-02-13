// Package proxy defines HTTP proxy configuration types used across applier implementations.
package proxy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	// ConfigHashKey is the annotation key used to record the proxy configuration hash.
	// This annotation is set on both ClusterExtensions and ClusterExtensionRevisions.
	// When the hash changes on a ClusterExtension, it triggers reconciliation.
	// Comparing hashes between ClusterExtension and its current revision determines
	// if a new revision is needed due to proxy configuration changes.
	ConfigHashKey = "olm.operatorframework.io/proxy-config-hash"
)

// Proxy holds HTTP proxy configuration values that are applied to rendered resources.
// These values are typically set as environment variables on generated Pods to enable
// operators to function correctly in environments that require HTTP proxies for outbound
// connections.
type Proxy struct {
	// HTTPProxy is the HTTP proxy URL (e.g., "http://proxy.example.com:8080").
	// An empty value means no HTTP proxy is configured.
	HTTPProxy string
	// HTTPSProxy is the HTTPS proxy URL (e.g., "https://proxy.example.com:8443").
	// An empty value means no HTTPS proxy is configured.
	HTTPSProxy string
	// NoProxy is a comma-separated list of hosts, domains, or CIDR ranges that should
	// bypass the proxy (e.g., "localhost,127.0.0.1,.cluster.local").
	// An empty value means all traffic will use the proxy (if configured).
	NoProxy string
	// fingerprint is a cached hash of the proxy configuration, calculated once during construction.
	// This is used to detect when proxy settings change and a new revision is needed.
	fingerprint string
}

// NewFromEnv creates a new Proxy from environment variables.
// Returns nil if no proxy environment variables are set.
// The fingerprint is calculated once during construction and cached.
func NewFromEnv() *Proxy {
	httpProxy := os.Getenv("HTTP_PROXY")
	httpsProxy := os.Getenv("HTTPS_PROXY")
	noProxy := os.Getenv("NO_PROXY")

	// If no proxy variables are set, return nil
	if httpProxy == "" && httpsProxy == "" && noProxy == "" {
		return nil
	}

	p := &Proxy{
		HTTPProxy:  httpProxy,
		HTTPSProxy: httpsProxy,
		NoProxy:    noProxy,
	}

	// Calculate and cache the fingerprint
	p.fingerprint = calculateFingerprint(p)

	return p
}

// calculateFingerprint computes a stable hash of the proxy configuration.
func calculateFingerprint(p *Proxy) string {
	if p == nil {
		return ""
	}
	data, err := json.Marshal(p)
	if err != nil {
		// This should never happen for a simple struct with string fields,
		// but return empty string if it does
		return ""
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8])
}

// Fingerprint returns the cached hash of the proxy configuration.
// This is used to detect when proxy settings change and a new revision is needed.
// Returns an empty string if the proxy is nil.
func (p *Proxy) Fingerprint() string {
	if p == nil {
		return ""
	}
	return p.fingerprint
}

// SanitizeURL removes credentials from a proxy URL for safe logging.
// Returns the original string if it's not a valid URL or doesn't contain credentials.
// If the string contains @ but credentials can't be parsed out, returns a redacted version.
func SanitizeURL(proxyURL string) string {
	if proxyURL == "" {
		return ""
	}

	u, err := url.Parse(proxyURL)
	if err != nil {
		// If we can't parse it, check if it might contain credentials (user:pass@host pattern)
		// If so, redact it to avoid leaking credentials in logs
		if strings.Contains(proxyURL, "@") {
			return "<redacted>"
		}
		// Otherwise return as-is (might be a hostname or other format without credentials)
		return proxyURL
	}

	// If there's user info, remove it and return sanitized URL
	if u.User != nil {
		u.User = nil
		return u.String()
	}

	// If no user info was parsed but the string contains @, it might be a schemelessly-formatted
	// URL like "user:pass@host:port" which url.Parse doesn't recognize as having credentials.
	// Redact it to be safe.
	if strings.Contains(proxyURL, "@") {
		return "<redacted>"
	}

	// No credentials detected, return as-is
	return proxyURL
}
