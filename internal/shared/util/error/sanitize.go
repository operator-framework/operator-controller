package error

import (
	"errors"
	"net"
)

// SanitizeNetworkError returns the error message with ephemeral network details
// (such as source ports) removed. This ensures consistent error messages across
// retries, preventing unnecessary status condition updates when the only change
// is the ephemeral source port in a connection error.
//
// For example, an error like:
//
//	"dial tcp 10.0.0.1:52341->registry.example.com:443: connect: connection refused"
//
// becomes:
//
//	"dial tcp registry.example.com:443: connect: connection refused"
//
// If the error does not contain a net.OpError with a Source address, or if the
// error is nil, the original error message is returned unchanged.
func SanitizeNetworkError(err error) string {
	if err == nil {
		return ""
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		msg := "lookup " + dnsErr.Name
		if dnsErr.Server != "" {
			msg += " on " + dnsErr.Server
		}
		if dnsErr.IsTimeout {
			msg += ": i/o timeout"
		}
		return msg
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		msg := opErr.Op
		if opErr.Net != "" {
			msg += " " + opErr.Net
		}
		return msg + ": " + opErr.Err.Error()
	}

	return err.Error()
}
