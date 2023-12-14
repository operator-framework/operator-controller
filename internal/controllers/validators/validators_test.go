package validators_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/operator-framework/operator-controller/api/v1alpha1"
	"github.com/operator-framework/operator-controller/internal/controllers/validators"
)

var semVers = []struct {
	data    string
	comment string
	result  bool
}{
	// list of valid semvers
	{"1.2.3", "simple semver", true},
	{"", "empty semver", true},
	{"1.2.3-alpha.1+metadata", "semver with pre-release and metadata", true},
	{"1.2.3-alpha-beta", "semver with pre-release", true},
	{">=1.2.3", ">= operator", true},
	{"=>1.2.3", "=> operator", true},
	{">= 1.2.3", ">= operator with space", true},
	{">=v1.2.3", ">= operator with 'v' prefix", true},
	{">= v1.2.3", ">= operator with space and 'v' prefix", true},
	{"<=1.2.3", "<= operator", true},
	{"=<1.2.3", "=< operator", true},
	{"=1.2.3", "= operator", true},
	{"!=1.2.3", "!= operator", true},
	{"<1.2.3", "< operator", true},
	{">1.2.3", "> operator", true},
	{"~1.2.2", "~ operator", true},
	{"~>1.2.3", "~> operator", true},
	{"^1.2.3", "^ operator", true},
	{"v1.2.3", "with 'v' prefix", true},
	{"1.x", "with lower-case y-stream", true},
	{"1.X", "with upper-case Y-stream", true},
	{"1.*", "with asterisk y-stream", true},
	{"1.2.x", "with lower-case z-stream", true},
	{"1.2.X", "with upper-case Z-stream", true},
	{"1.2.*", "with asterisk z-stream", true},
	{">=1.2.3 <2.3.4", "multiple operators space-separated", true},
	{">=1.2.3,<2.3.4", "multiple operators comma-separated", true},
	{">=1.2.3, <2.3.4", "multiple operators comma-and-space-separated", true},
	{"<1.2.3||>2.3.4", "multiple operators OR-separated", true},
	{"<1.2.3|| >2.3.4", "multiple operarors OR-and-space-separated", true},
	{"<1.2.3 ||>2.3.4", "multiple operators space-and-OR-separated", true},
	{"<1.2.3 || >2.3.4", "multiple operators space-and-OR-and-space-separated", true},
	{">1.0.0,<1.2.3 || >2.1.0", "multiple operators with comma and OR separation", true},
	{"<1.2.3-abc >2.3.4-def", "multiple operators with pre-release data", true},
	{"<1.2.3-abc+def >2.3.4-ghi+jkl", "multiple operators with pre-release and metadata", true},
	// list of invalid semvers
	{"invalid-semver", "invalid characters", false},
	{"1.2.3.4", "too many components", false},
	{"1.2.3-beta!", "invalid character in pre-release", false},
	{"1.2.3.alpha", "invalid pre-release/4th component", false},
	{"1..2.3", "extra dot", false},
	{"1.2.3-pre+bad_metadata", "invalid metadata", false},
	{"1.2.-3", "negative component", false},
	{".1.2.3", "leading dot", false},
	{"<<1.2.3", "invalid << operator", false},
	{">>1.2.3", "invalid >> operator", false},
	{">~1.2.3", "invalid >~ operator", false},
	{"==1.2.3", "invalid == operator, valid for blang", false},
	{"=!1.2.3", "invalid =! operator", false},
	{"!1.2.3", "invalid ! operator, valid for blang", false},
	{"1.Y", "invalid y-stream wildcard", false},
	{">1.2.3 && <2.3.4", "invalid AND separator", false},
	{">1.2.3;<2.3.4", "invalid semicolon separator", false},
	// Invalid semvers that get past simple validation - THESE NEED TO BE TESTED SEPARATELY
	{"1.02.3", "leading zero in y-stream - FALSE POSITIVE", true},
	{"1.2.03", "leading zero in z-stream - FALSE POSITIVE", true},
	{"1.2.3 - 2.3.4", "unsupported hyphen (range) operator - FALSE POSITIVE", true},
}

func TestValidateClusterExtensionSpecSemVer(t *testing.T) {
	t.Parallel()
	for _, s := range semVers {
		d := s
		t.Run(d.comment, func(t *testing.T) {
			t.Parallel()
			clusterExtension := &v1alpha1.ClusterExtension{
				Spec: v1alpha1.ClusterExtensionSpec{
					Version: d.data,
				},
			}
			if d.result {
				require.NoError(t, validators.ValidateClusterExtensionSpec(clusterExtension))
			} else {
				require.Error(t, validators.ValidateClusterExtensionSpec(clusterExtension))
			}
		})
	}
}
