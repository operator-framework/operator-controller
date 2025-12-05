package controllers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1 "github.com/operator-framework/operator-controller/api/v1"
)

func TestClusterExtensionSourceConfig(t *testing.T) {
	sourceTypeEmptyError := "Invalid value: null"
	sourceTypeMismatchError := "spec.source.sourceType: Unsupported value"
	sourceConfigInvalidError := "spec.source: Invalid value"
	// unionField represents the required Catalog or (future) Bundle field required by SourceConfig
	testCases := []struct {
		name       string
		sourceType string
		unionField string
		errMsg     string
	}{
		{"sourceType is null", "", "Catalog", sourceTypeEmptyError},
		{"sourceType is invalid", "Invalid", "Catalog", sourceTypeMismatchError},
		{"catalog field does not exist", "Catalog", "", sourceConfigInvalidError},
		{"sourceConfig has required fields", "Catalog", "Catalog", ""},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			var err error
			if tc.unionField == "Catalog" {
				err = cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: tc.sourceType,
						Catalog: &ocv1.CatalogFilter{
							PackageName: "test-package",
						},
					},
					Namespace: "default",
					ServiceAccount: ocv1.ServiceAccountReference{
						Name: "default",
					},
				}))
			}
			if tc.unionField == "" {
				err = cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
					Source: ocv1.SourceConfig{
						SourceType: tc.sourceType,
					},
					Namespace: "default",
					ServiceAccount: ocv1.ServiceAccountReference{
						Name: "default",
					},
				}))
			}

			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for sourceType %q: %w", tc.sourceType, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func TestClusterExtensionAdmissionPackageName(t *testing.T) {
	tooLongError := "spec.source.catalog.packageName: Too long: may not be more than 253"
	regexMismatchError := "packageName must be a valid DNS1123 subdomain"

	testCases := []struct {
		name    string
		pkgName string
		errMsg  string
	}{
		{"no package name", "", regexMismatchError},
		{"long package name", strings.Repeat("x", 254), tooLongError},
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
		{"dot separated", "some.package", ""},
		{"underscore separated", "some_package", regexMismatchError},
		{"starts with dot", ".some.package", regexMismatchError},
		{"multiple sequential separators", "a.-b", regexMismatchError},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: tc.pkgName,
					},
				},
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
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
	tooLongError := "spec.source.catalog.version: Too long: may not be more than 64"
	regexMismatchError := "invalid version expression"

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
			err := cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "package",
						Version:     tc.version,
					},
				},
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
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
	tooLongError := "spec.source.catalog.channels[0]: Too long: may not be more than 253"
	regexMismatchError := "channels entries must be valid DNS1123 subdomains"

	testCases := []struct {
		name     string
		channels []string
		errMsg   string
	}{
		{"no channel name", []string{""}, regexMismatchError},
		{"hypen-separated", []string{"hyphenated-name"}, ""},
		{"dot-separated", []string{"dotted.name"}, ""},
		{"includes version", []string{"channel-has-version-1.0.1"}, ""},
		{"long channel name", []string{strings.Repeat("x", 254)}, tooLongError},
		{"spaces", []string{"spaces spaces"}, regexMismatchError},
		{"capitalized", []string{"Capitalized"}, regexMismatchError},
		{"camel case", []string{"camelCase"}, regexMismatchError},
		{"invalid characters", []string{"many/invalid$characters+in_name"}, regexMismatchError},
		{"starts with hyphen", []string{"-start-with-hyphen"}, regexMismatchError},
		{"ends with hyphen", []string{"end-with-hyphen-"}, regexMismatchError},
		{"starts with period", []string{".start-with-period"}, regexMismatchError},
		{"ends with period", []string{"end-with-period."}, regexMismatchError},
		{"contains underscore", []string{"some_thing"}, regexMismatchError},
		{"multiple sequential separators", []string{"a.-b"}, regexMismatchError},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "package",
						Channels:    tc.channels,
					},
				},
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for channel %q: %w", tc.channels, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func TestClusterExtensionAdmissionInstallNamespace(t *testing.T) {
	tooLongError := "spec.namespace: Too long: may not be more than 63"
	regexMismatchError := "namespace must be a valid DNS1123 label"

	testCases := []struct {
		name      string
		namespace string
		errMsg    string
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
			err := cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "package",
					},
				},
				Namespace: tc.namespace,
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for namespace %q: %w", tc.namespace, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func TestClusterExtensionAdmissionServiceAccount(t *testing.T) {
	tooLongError := "spec.serviceAccount.name: Too long: may not be more than 253"
	regexMismatchError := "name must be a valid DNS1123 subdomain"

	testCases := []struct {
		name           string
		serviceAccount string
		errMsg         string
	}{
		{"just alphanumeric", "justalphanumeric1", ""},
		{"hypen-separated", "hyphenated-name", ""},
		{"dot-separated", "dotted.name", ""},
		{"longest valid service account name", strings.Repeat("x", 253), ""},
		{"too long service account name", strings.Repeat("x", 254), tooLongError},
		{"no service account name", "", regexMismatchError},
		{"spaces", "spaces spaces", regexMismatchError},
		{"capitalized", "Capitalized", regexMismatchError},
		{"camel case", "camelCase", regexMismatchError},
		{"invalid characters", "many/invalid$characters+in_name", regexMismatchError},
		{"starts with hyphen", "-start-with-hyphen", regexMismatchError},
		{"ends with hyphen", "end-with-hyphen-", regexMismatchError},
		{"starts with period", ".start-with-period", regexMismatchError},
		{"ends with period", "end-with-period.", regexMismatchError},
		{"multiple sequential separators", "a.-b", regexMismatchError},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "package",
					},
				},
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: tc.serviceAccount,
				},
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for service account name %q: %w", tc.serviceAccount, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func TestClusterExtensionAdmissionInstall(t *testing.T) {
	oneOfErrMsg := "at least one of [preflight] are required when install is specified"

	testCases := []struct {
		name          string
		installConfig *ocv1.ClusterExtensionInstallConfig
		errMsg        string
	}{
		{
			name:          "install specified, nothing configured",
			installConfig: &ocv1.ClusterExtensionInstallConfig{},
			errMsg:        oneOfErrMsg,
		},
		{
			name: "install specified, preflight configured",
			installConfig: &ocv1.ClusterExtensionInstallConfig{
				Preflight: &ocv1.PreflightConfig{
					CRDUpgradeSafety: &ocv1.CRDUpgradeSafetyPreflightConfig{
						Enforcement: ocv1.CRDUpgradeSafetyEnforcementNone,
					},
				},
			},
			errMsg: "",
		},
		{
			name:          "install not specified",
			installConfig: nil,
			errMsg:        "",
		},
	}

	t.Parallel()
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "package",
					},
				},
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
				Install: tc.installConfig,
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for install configuration %v: %w", tc.installConfig, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func Test_ClusterExtensionAdmissionInlineConfig(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		configBytes []byte
		errMsg      string
	}{
		{
			name:        "rejects valid json that is not of an object type",
			configBytes: []byte(`true`),
			errMsg:      "spec.config.inline in body must be of type object",
		},
		{
			name:        "rejects empty json object",
			configBytes: []byte(`{}`),
			errMsg:      "spec.config.inline in body should have at least 1 properties",
		},
		{
			name:        "accepts valid json object with configuration",
			configBytes: []byte(`{"key": "value"}`),
		},
		{
			name:        "accepts valid json object with nested configuration",
			configBytes: []byte(`{"key": {"foo": ["bar", "baz"], "h4x0r": 1337}}`),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(context.Background(), buildClusterExtension(ocv1.ClusterExtensionSpec{
				Source: ocv1.SourceConfig{
					SourceType: "Catalog",
					Catalog: &ocv1.CatalogFilter{
						PackageName: "package",
					},
				},
				Namespace: "default",
				ServiceAccount: ocv1.ServiceAccountReference{
					Name: "default",
				},
				Config: &ocv1.ClusterExtensionConfig{
					ConfigType: ocv1.ClusterExtensionConfigTypeInline,
					Inline: &apiextensionsv1.JSON{
						Raw: tc.configBytes,
					},
				},
			}))
			if tc.errMsg == "" {
				require.NoError(t, err, "unexpected error for inline bundle configuration %q: %w", string(tc.configBytes), err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			}
		})
	}
}

func buildClusterExtension(spec ocv1.ClusterExtensionSpec) *ocv1.ClusterExtension {
	return &ocv1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-extension-",
		},
		Spec: spec,
	}
}
