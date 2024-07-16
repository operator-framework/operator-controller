package error

import (
	"fmt"
	"regexp"
)

var (
	installConflictErrorPattern = regexp.MustCompile(`Unable to continue with install: (\w+) "(.*)" in namespace "(.*)" exists and cannot be imported into the current release.*`)
)

type Olmv1Err struct {
	originalErr error
	message     string
}

func (o Olmv1Err) Error() string {
	return o.message
}

func (o Olmv1Err) Cause() error {
	return o.originalErr
}

func newOlmv1Err(originalErr error, message string) error {
	return &Olmv1Err{
		originalErr: originalErr,
		message:     message,
	}
}

func AsOlmErr(originalErr error) error {
	if originalErr == nil {
		return nil
	}

	for _, exec := range rules {
		if err := exec(originalErr); err != nil {
			return err
		}
	}

	// let's mark any unmatched errors as unknown
	return defaultErrTranslator(originalErr)
}

// rule is a function that translates an error into a more specific error
// typically to hide internal implementation details
// in: helm error
// out: nil -> no translation | !nil -> translated error
type rule func(originalErr error) error

// rules is a list of rules for error translation
var rules = []rule{
	helmInstallConflictErr,
}

// installConflictErrorTranslator
func helmInstallConflictErr(originalErr error) error {
	matches := installConflictErrorPattern.FindStringSubmatch(originalErr.Error())
	if len(matches) != 4 {
		// there was no match
		return nil
	}
	kind := matches[1]
	name := matches[2]
	namespace := matches[3]
	return newOlmv1Err(originalErr, fmt.Sprintf("%s '%s' already exists in namespace '%s' and cannot be managed by operator-controller", kind, name, namespace))
}

func defaultErrTranslator(originalErr error) error {
	return originalErr
}
