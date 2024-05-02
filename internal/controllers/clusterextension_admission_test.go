package controllers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func TestClusterExtensionAdmissionPackageName(t *testing.T) {
	tooLongError := "spec.packageName: Too long: may not be longer than 48"
	regexMismatchError := "spec.packageName in body should match"

	testCases := []struct {
		name    string
		pkgName string
		errMsg  string
	}{
		{"no package name", "", regexMismatchError},
		{"long package name", "this-is-a-really-long-package-name-that-is-greater-than-48-characters", tooLongError},
		{"leading digits with hypens", "0my-1package-9name", ""},
		{"trailing digits with hypens", "my0-package1-name9", ""},
		{"digits with hypens", "012-345-678-9", ""},
		{"letters with hypens", "abc-def-ghi-jkl", ""},
		{"letters only", "abcdefghi", ""},
		{"letters with digits", "abc123def456ghi789", ""},
		{"digits only", "1234567890", ""},
		{"single character", "a", ""},
		{"single digit", "1", ""},
		{"single hypen", "-", regexMismatchError},
		{"uppercase letters", "ABC-DEF-GHI-JKL", regexMismatchError},
		{"special characters", "my-$pecial-package-name", regexMismatchError},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName:      tc.pkgName,
				InstallNamespace: "default",
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for package name %q: %w", tc.pkgName, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}
func TestClusterExtensionAdmissionVersion(t *testing.T) {
	tooLongError := "spec.version: Too long: may not be longer than 64"
	regexMismatchError := "spec.version in body should match"

	testCases := []struct {
		name    string
		version string
		errMsg  string
	}{
		{"empty semver", "", ""},
		{"simple semver", "1.2.3", ""},
		{"semver with pre-release and metadata", "1.2.3-alpha.1+metadata", ""},
		{"semver with pre-release", "1.2.3-alpha-beta", ""},
		{">= operator", ">=1.2.3", ""},
		{"=> operator", "=>1.2.3", ""},
		{">= operator with space", ">= 1.2.3", ""},
		{">= operator with 'v' prefix", ">=v1.2.3", ""},
		{">= operator with space and 'v' prefix", ">= v1.2.3", ""},
		{"<= operator", "<=1.2.3", ""},
		{"=< operator", "=<1.2.3", ""},
		{"= operator", "=1.2.3", ""},
		{"!= operator", "!=1.2.3", ""},
		{"< operator", "<1.2.3", ""},
		{"> operator", ">1.2.3", ""},
		{"~ operator", "~1.2.2", ""},
		{"~> operator", "~>1.2.3", ""},
		{"^ operator", "^1.2.3", ""},
		{"with 'v' prefix", "v1.2.3", ""},
		{"with lower-case y-stream", "1.x", ""},
		{"with upper-case Y-stream", "1.X", ""},
		{"with asterisk y-stream", "1.*", ""},
		{"with lower-case z-stream", "1.2.x", ""},
		{"with upper-case Z-stream", "1.2.X", ""},
		{"with asterisk z-stream", "1.2.*", ""},
		{"multiple operators space-separated", ">=1.2.3 <2.3.4", ""},
		{"multiple operators comma-separated", ">=1.2.3,<2.3.4", ""},
		{"multiple operators comma-and-space-separated", ">=1.2.3, <2.3.4", ""},
		{"multiple operators OR-separated", "<1.2.3||>2.3.4", ""},
		{"multiple operarors OR-and-space-separated", "<1.2.3|| >2.3.4", ""},
		{"multiple operators space-and-OR-separated", "<1.2.3 ||>2.3.4", ""},
		{"multiple operators space-and-OR-and-space-separated", "<1.2.3 || >2.3.4", ""},
		{"multiple operators with comma and OR separation", ">1.0.0,<1.2.3 || >2.1.0", ""},
		{"multiple operators with pre-release data", "<1.2.3-abc >2.3.4-def", ""},
		{"multiple operators with pre-release and metadata", "<1.2.3-abc+def >2.3.4-ghi+jkl", ""},
		// list of invalid semvers
		{"invalid characters", "invalid-semver", regexMismatchError},
		{"too many components", "1.2.3.4", regexMismatchError},
		{"invalid character in pre-release", "1.2.3-beta!", regexMismatchError},
		{"invalid pre-release/4th component", "1.2.3.alpha", regexMismatchError},
		{"extra dot", "1..2.3", regexMismatchError},
		{"invalid metadata", "1.2.3-pre+bad_metadata", regexMismatchError},
		{"negative component", "1.2.-3", regexMismatchError},
		{"leading dot", ".1.2.3", regexMismatchError},
		{"invalid << operator", "<<1.2.3", regexMismatchError},
		{"invalid >> operator", ">>1.2.3", regexMismatchError},
		{"invalid >~ operator", ">~1.2.3", regexMismatchError},
		{"invalid == operator", "==1.2.3", regexMismatchError},
		{"invalid =! operator", "=!1.2.3", regexMismatchError},
		{"invalid ! operator", "!1.2.3", regexMismatchError},
		{"invalid y-stream wildcard", "1.Y", regexMismatchError},
		{"invalid AND separator", ">1.2.3 && <2.3.4", regexMismatchError},
		{"invalid semicolon separator", ">1.2.3;<2.3.4", regexMismatchError},
		{"leading zero in x-stream", "01.2.3", regexMismatchError},
		{"leading zero in y-stream", "1.02.3", regexMismatchError},
		{"leading zero in z-stream", "1.2.03", regexMismatchError},
		{"unsupported hyphen (range) operator", "1.2.3 - 2.3.4", regexMismatchError},
		{"valid semver, but too long", "1234567890.1234567890.12345678901234567890123456789012345678901234", tooLongError},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName:      "package",
				Version:          tc.version,
				InstallNamespace: "default",
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for version %q: %w", tc.version, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func TestClusterExtensionAdmissionChannel(t *testing.T) {
	tooLongError := "spec.channel: Too long: may not be longer than 48"
	regexMismatchError := "spec.channel in body should match"

	testCases := []struct {
		name        string
		channelName string
		errMsg      string
	}{
		{"no channel name", "", ""},
		{"hypen-separated", "hyphenated-name", ""},
		{"dot-separated", "dotted.name", ""},
		{"includes version", "channel-has-version-1.0.1", ""},
		{"long channel name", "longname01234567890123456789012345678901234567890", tooLongError},
		{"spaces", "spaces spaces", regexMismatchError},
		{"capitalized", "Capitalized", regexMismatchError},
		{"camel case", "camelCase", regexMismatchError},
		{"invalid characters", "many/invalid$characters+in_name", regexMismatchError},
		{"starts with hyphen", "-start-with-hyphen", regexMismatchError},
		{"ends with hyphen", "end-with-hyphen-", regexMismatchError},
		{"starts with period", ".start-with-period", regexMismatchError},
		{"ends with period", "end-with-period.", regexMismatchError},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName:      "package",
				Channel:          tc.channelName,
				InstallNamespace: "default",
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for channel %q: %w", tc.channelName, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func TestClusterExtensionAdmissionInstallNamespace(t *testing.T) {
	tooLongError := "spec.installNamespace: Too long: may not be longer than 63"
	regexMismatchError := "spec.installNamespace in body should match"

	testCases := []struct {
		name             string
		installNamespace string
		errMsg           string
	}{
		{"just alphanumeric", "justalphanumberic1", ""},
		{"hypen-separated", "hyphenated-name", ""},
		{"no install namespace", "", regexMismatchError},
		{"dot-separated", "dotted.name", regexMismatchError},
		{"longest valid install namespace", strings.Repeat("x", 63), ""},
		{"too long install namespace name", strings.Repeat("x", 64), tooLongError},
		{"spaces", "spaces spaces", regexMismatchError},
		{"capitalized", "Capitalized", regexMismatchError},
		{"camel case", "camelCase", regexMismatchError},
		{"invalid characters", "many/invalid$characters+in_name", regexMismatchError},
		{"starts with hyphen", "-start-with-hyphen", regexMismatchError},
		{"ends with hyphen", "end-with-hyphen-", regexMismatchError},
		{"starts with period", ".start-with-period", regexMismatchError},
		{"ends with period", "end-with-period.", regexMismatchError},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName:      "package",
				InstallNamespace: tc.installNamespace,
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for installNamespace %q: %w", tc.installNamespace, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func buildClusterExtension(spec ocv1alpha1.ClusterExtensionSpec) *ocv1alpha1.ClusterExtension {
	return &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-extension-",
		},
		Spec: spec,
	}
}
