package main

import (
	"github.com/go-logr/logr"
)

func testLogger() {
	var logger logr.Logger
	var err error
	var value int

	// Case 1: Nil error - Ensures the first argument cannot be nil.
	logger.Error(nil, "message") // want ".*may hide error details, making debugging harder*"

	// Case 2: Odd number of key-value arguments - Ensures key-value pairs are complete.
	logger.Error(err, "message", "key1") // want ".*Key-value pairs must be provided after the message, but an odd number of arguments was found.*"

	// Case 3: Key in key-value pair is not a string - Ensures keys in key-value pairs are strings.
	logger.Error(err, "message", 123, value) // want ".*Ensure keys are strings.*"

	// Case 4: Values are passed without corresponding keys - Ensures key-value arguments are structured properly.
	logger.Error(err, "message", value, "key2", value) // want ".*Key-value pairs must be provided after the message, but an odd number of arguments was found.*"

	// Case 5: Correct Usage - Should not trigger any warnings.
	logger.Error(err, "message", "key1", value, "key2", "value")
}
