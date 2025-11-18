package server

import (
	"net/http"
	"time"
)

// checkPreconditions checks HTTP preconditions (If-Modified-Since, If-Unmodified-Since)
// Returns true if the request has already been handled (e.g., 304 Not Modified response sent)
func checkPreconditions(w http.ResponseWriter, r *http.Request, modtime time.Time) (done bool) {
	// Check If-Modified-Since
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if t, err := time.Parse(http.TimeFormat, r.Header.Get("If-Modified-Since")); err == nil {
			// The Last-Modified header truncates sub-second precision, so
			// use ModTime < t+1s instead of ModTime <= t to check for unmodified.
			if modtime.Before(t.Add(time.Second)) {
				w.WriteHeader(http.StatusNotModified)
				return true
			}
		}
	}

	// Check If-Unmodified-Since
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		if t, err := time.Parse(http.TimeFormat, r.Header.Get("If-Unmodified-Since")); err == nil {
			// The Last-Modified header truncates sub-second precision, so
			// use ModTime >= t+1s instead of ModTime > t to check for modified.
			if modtime.After(t.Add(-time.Second)) {
				w.WriteHeader(http.StatusPreconditionFailed)
				return true
			}
		}
	}

	return false
}
