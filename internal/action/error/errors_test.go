package error

import (
	"errors"
	"testing"
)

func TestAsOlmErr(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{
			name:     "Install conflict error (match)",
			err:      errors.New("Unable to continue with install: Deployment \"my-deploy\" in namespace \"my-namespace\" exists and cannot be imported into the current release"),
			expected: errors.New("Deployment 'my-deploy' already exists in namespace 'my-namespace' and cannot be managed by operator-controller"),
		},
		{
			name:     "Install conflict error (no match)",
			err:      errors.New("Unable to continue with install: because of something"),
			expected: errors.New("Unable to continue with install: because of something"),
		},
		{
			name:     "Unknown error",
			err:      errors.New("some unknown error"),
			expected: errors.New("some unknown error"),
		},
		{
			name:     "Nil error",
			err:      nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AsOlmErr(tt.err)
			if result != nil && result.Error() != tt.expected.Error() {
				t.Errorf("Expected: %v, got: %v", tt.expected, result)
			}
		})
	}
}
