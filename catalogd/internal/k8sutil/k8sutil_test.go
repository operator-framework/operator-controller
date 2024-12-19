package k8sutil

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMetadataName(t *testing.T) {
	type testCase struct {
		name           string
		in             string
		expectedResult string
		expectedValid  bool
	}
	for _, tc := range []testCase{
		{
			name:           "empty",
			in:             "",
			expectedResult: "",
			expectedValid:  false,
		},
		{
			name:           "invalid",
			in:             "foo-bar.123!",
			expectedResult: "foo-bar.123-",
			expectedValid:  false,
		},
		{
			name:           "too long",
			in:             fmt.Sprintf("foo-bar_%s", strings.Repeat("1234567890", 50)),
			expectedResult: fmt.Sprintf("foo-bar-%s", strings.Repeat("1234567890", 50)),
			expectedValid:  false,
		},
		{
			name:           "valid",
			in:             "foo-bar.123",
			expectedResult: "foo-bar.123",
			expectedValid:  true,
		},
		{
			name:           "valid with underscore",
			in:             "foo-bar_123",
			expectedResult: "foo-bar-123",
			expectedValid:  true,
		},
		{
			name:           "valid with colon",
			in:             "foo-bar:123",
			expectedResult: "foo-bar-123",
			expectedValid:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actualResult, actualValid := MetadataName(tc.in)
			assert.Equal(t, tc.expectedResult, actualResult)
			assert.Equal(t, tc.expectedValid, actualValid)
		})
	}
}
