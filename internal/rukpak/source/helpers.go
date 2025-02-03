package source

import (
	"errors"
	"os"
	"time"
)

// IsImageUnpacked checks whether an image has been unpacked in `unpackPath`.
// If true, time of unpack will also be returned. If false unpack time is gibberish (zero/epoch time).
// If `unpackPath` is a file, it will be deleted and false will be returned without an error.
func IsImageUnpacked(unpackPath string) (bool, time.Time, error) {
	unpackStat, err := os.Stat(unpackPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	if !unpackStat.IsDir() {
		return false, time.Time{}, os.Remove(unpackPath)
	}
	return true, unpackStat.ModTime(), nil
}
