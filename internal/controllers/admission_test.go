package controllers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func buildClusterExtension(spec ocv1alpha1.ClusterExtensionSpec) *ocv1alpha1.ClusterExtension {
	return &ocv1alpha1.ClusterExtension{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-extension",
		},
		Spec: spec,
	}
}

var clusterExtensionData = []struct {
	spec    *ocv1alpha1.ClusterExtension
	comment string
	errMsg  string
}{
	{
		buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{}),
		"spec is empty",
		"spec.packageName in body should match '^[a-z0-9]+(-[a-z0-9]+)*$'",
	},
	{
		buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
			PackageName: "this-is-a-really-long-package-name-that-is-greater-than-48-characters",
		}),
		"long package name",
		"spec.packageName: Too long: may not be longer than 48",
	},
	{
		buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
			PackageName: "package",
			Version:     "1234567890.1234567890.12345678901234567890123456789012345678901234",
		}),
		"long valid semver",
		"spec.version: Too long: may not be longer than 64",
	},
	{
		buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
			PackageName: "package",
			Channel:     "longname01234567890123456789012345678901234567890",
		}),
		"long channel name",
		"spec.channel: Too long: may not be longer than 48",
	},
}

func TestClusterExtensionSpecs(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	for _, ed := range clusterExtensionData {
		d := ed
		t.Run(d.comment, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(ctx, d.spec)
			require.Error(t, err)
			require.ErrorContains(t, err, d.errMsg)
		})
	}
}

func TestClusterExtensionInvalidSemver(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	invalidSemvers := []string{
		"1.2.3.4",
		"1.02.3",
		"1.2.03",
		"1.2.3-beta!",
		"1.2.3.alpha",
		"1..2.3",
		"1.2.3-pre+bad_metadata",
		"1.2.-3",
		".1.2.3",
		"<<1.2.3",
		">>1.2.3",
		">~1.2.3",
		"==1.2.3",
		"=!1.2.3",
		"!1.2.3",
		"1.Y",
		">1.2.3 && <2.3.4",
		">1.2.3;<2.3.4",
		"1.2.3 - 2.3.4",
	}
	for _, sm := range invalidSemvers {
		d := sm
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(ctx, buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName: "package",
				Version:     d,
			}))
			require.Errorf(t, err, "expected error for invalid semver %q", d)
			// Don't need to include the whole regex, this should be enough to match the MasterMinds regex
			require.ErrorContains(t, err, "spec.version in body should match '^(\\s*(=||!=|>|<|>=|=>|<=|=<|~|~>|\\^)")
		})
	}
}

func TestClusterExtensionValidSemver(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	validSemvers := []string{
		">=1.2.3",
		"=>1.2.3",
		">= 1.2.3",
		">=v1.2.3",
		">= v1.2.3",
		"<=1.2.3",
		"=<1.2.3",
		"=1.2.3",
		"!=1.2.3",
		"<1.2.3",
		">1.2.3",
		"~1.2.2",
		"~>1.2.3",
		"^1.2.3",
		"1.2.3",
		"v1.2.3",
		"1.x",
		"1.X",
		"1.*",
		"1.2.x",
		"1.2.X",
		"1.2.*",
		">=1.2.3 <2.3.4",
		">=1.2.3,<2.3.4",
		">=1.2.3, <2.3.4",
		"<1.2.3||>2.3.4",
		"<1.2.3|| >2.3.4",
		"<1.2.3 ||>2.3.4",
		"<1.2.3 || >2.3.4",
		">1.0.0,<1.2.3 || >2.1.0",
		"<1.2.3-abc >2.3.4-def",
		"<1.2.3-abc+def >2.3.4-ghi+jkl",
	}
	for _, smx := range validSemvers {
		d := smx
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			op := buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName: "package",
				Version:     d,
			})
			err := cl.Create(ctx, op)
			require.NoErrorf(t, err, "unexpected error for semver range %q: %w", d, err)
		})
	}
}

func TestClusterExtensionInvalidChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	invalidChannels := []string{
		"spaces spaces",
		"Capitalized",
		"camelCase",
		"many/invalid$characters+in_name",
		"-start-with-hyphen",
		"end-with-hyphen-",
		".start-with-period",
		"end-with-period.",
	}
	for _, ch := range invalidChannels {
		d := ch
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(ctx, buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName: "package",
				Channel:     d,
			}))
			require.Errorf(t, err, "expected error for invalid channel %q", d)
			require.ErrorContains(t, err, "spec.channel in body should match '^[a-z0-9]+([\\.-][a-z0-9]+)*$'")
		})
	}
}

func TestClusterExtensionValidChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	validChannels := []string{
		"hyphenated-name",
		"dotted.name",
		"channel-has-version-1.0.1",
	}
	for _, ch := range validChannels {
		d := ch
		t.Run(d, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			ext := buildClusterExtension(ocv1alpha1.ClusterExtensionSpec{
				PackageName: "package",
				Channel:     d,
			})
			err := cl.Create(ctx, ext)
			require.NoErrorf(t, err, "unexpected error creating valid channel %q: %w", d, err)
		})
	}
}
