package error

import (
	"regexp"
)

var ephemeralNetworkErrorPattern = regexp.MustCompile(`(read|write) (tcp|udp) ((?:[0-9]{1,3}(?:\.[0-9]{1,3}){3}|\[[0-9a-fA-F:]+\])(?::\d+)?)->((?:[0-9]{1,3}(?:\.[0-9]{1,3}){3}|\[[0-9a-fA-F:]+\])(?::\d+)?)(: )?`)

// SanitizeNetworkError returns a stable, deterministic error message for network
// errors by stripping ephemeral details (such as source and destination addresses
// and ports) from low-level socket operations. This ensures consistent error
// messages across retries, preventing unnecessary status condition updates when
// the only change is an ephemeral source port.
//
// The function uses a regex to remove substrings matching the pattern
// "read/write tcp/udp <src>-><dst>: " (with IPv4 or IPv6 addresses), which
// commonly appear inside [net.DNSError] Err fields (e.g.,
// "read udp 10.244.0.8:46753->10.96.0.10:53: i/o timeout").
//
// Returns "" for nil errors, or the sanitized error string otherwise.
func SanitizeNetworkError(err error) string {
	if err == nil {
		return ""
	}
	return ephemeralNetworkErrorPattern.ReplaceAllString(err.Error(), "")
}
