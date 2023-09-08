package sort

import (
	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
)

// ByVersion is a sort "less" function that orders bundles
// in inverse version order (higher versions on top).
func ByVersion(b1, b2 *catalogmetadata.Bundle) bool {
	ver1, err1 := b1.Version()
	ver2, err2 := b2.Version()
	if err1 != nil || err2 != nil {
		return compareErrors(err1, err2) < 0
	}

	// Check for "greater than" because
	// we want higher versions on top
	return ver1.GT(*ver2)
}

// compareErrors returns 0 if both errors are either nil or not nil
// -1 if err1 is nil and err2 is not nil
// +1 if err1 is not nil and err2 is nil
func compareErrors(err1 error, err2 error) int {
	if err1 != nil && err2 == nil {
		return 1
	}

	if err1 == nil && err2 != nil {
		return -1
	}
	return 0
}
