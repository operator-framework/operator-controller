package storage

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"runtime"
	"runtime/debug"
	"strings"
)

// Instance is a storage instance that stores FBC content of catalogs
// added to a cluster. It can be used to Store or Delete FBC in the
// host's filesystem. It also a manager runnable object, that starts
// a server to serve the content stored.
type Instance interface {
	Store(ctx context.Context, catalog string, fsys fs.FS) error
	Delete(catalog string) error
	ForgetIndex(catalog string)
	ContentExists(catalog string) bool

	BaseURL(catalog string) string
	StorageServerHandler() http.Handler
}

// dumpStackAndMemStats logs memory stats and a partial stack trace for debugging
func dumpStackAndMemStats(prefix string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Log memory stats
	fmt.Printf("[MEMLEAK] %s - Memory Stats:\n", prefix)
	fmt.Printf("  Alloc: %v MiB\n", m.Alloc/1024/1024)
	fmt.Printf("  TotalAlloc: %v MiB\n", m.TotalAlloc/1024/1024)
	fmt.Printf("  Sys: %v MiB\n", m.Sys/1024/1024)
	fmt.Printf("  NumGC: %v\n", m.NumGC)
	fmt.Printf("  HeapObjects: %v\n", m.HeapObjects)

	// Get and log a condensed stack trace
	stack := debug.Stack()
	lines := strings.Split(string(stack), "\n")
	if len(lines) > 20 {
		lines = lines[:20] // Keep just the first 20 lines
	}
	fmt.Printf("[MEMLEAK] %s - Stack trace (truncated):\n%s\n", prefix, strings.Join(lines, "\n"))
}
