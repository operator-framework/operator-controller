package controllers

import (
	"strconv"
	"strings"
	"testing"
)

// ExtractRevisionNumber parses the revision number from a test revision name.
// It expects names to end with a numeric revision (e.g., "rev-1", "test-ext-10").
// Returns 1 as default if parsing fails, which is suitable for test fixtures.
//
// Note: This is a test helper and silently defaults to 1 for convenience.
// Callers are responsible for ensuring the name suffix matches the Spec.Revision value they intend to set;
// this function does not enforce or validate such a match.
func ExtractRevisionNumber(t *testing.T, name string) int64 {
	t.Helper()

	parts := strings.Split(name, "-")
	if len(parts) == 0 {
		t.Logf("warning: revision name %q has no parts, defaulting to revision 1", name)
		return 1
	}

	lastPart := parts[len(parts)-1]
	revNum, err := strconv.ParseInt(lastPart, 10, 64)
	if err != nil {
		t.Logf("warning: revision name %q does not end with a numeric revision (got %q), defaulting to revision 1. "+
			"Test helper names should follow the pattern 'prefix-<number>' (e.g., 'rev-1', 'test-ext-10')",
			name, lastPart)
		return 1
	}

	return revNum
}
