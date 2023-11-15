package controllers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorsv1alpha1 "github.com/operator-framework/operator-controller/api/v1alpha1"
)

func operator(spec operatorsv1alpha1.OperatorSpec) *operatorsv1alpha1.Operator {
	return &operatorsv1alpha1.Operator{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-operator",
		},
		Spec: spec,
	}
}

var operatorData = []struct {
	spec    *operatorsv1alpha1.Operator
	comment string
	errMsg  string
}{
	{
		operator(operatorsv1alpha1.OperatorSpec{}),
		"operator spec is empty",
		"spec.packageName in body should match '^[a-z0-9]+(-[a-z0-9]+)*$'",
	},
	{
		operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "this-is-a-really-long-package-name-that-is-greater-than-48-characters",
		}),
		"long package name",
		"spec.packageName: Too long: may not be longer than 48",
	},
	{
		operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Version:     "1234567890.1234567890.12345678901234567890123456789012345678901234",
		}),
		"long valid semver",
		"spec.version: Too long: may not be longer than 64",
	},
	{
		operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Channel:     "longname01234567890123456789012345678901234567890",
		}),
		"long channel name",
		"spec.channel: Too long: may not be longer than 48",
	},
}

func TestOperatorSpecs(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	for _, od := range operatorData {
		d := od
		t.Run(d.comment, func(t *testing.T) {
			t.Parallel()
			cl := newClient(t)
			err := cl.Create(ctx, d.spec)
			require.Error(t, err)
			require.ErrorContains(t, err, d.errMsg)
		})
	}
}

func TestOperatorInvalidSemver(t *testing.T) {
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
			err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
				PackageName: "package",
				Version:     d,
			}))
			require.Errorf(t, err, "expected error for invalid semver %q", d)
			// Don't need to include the whole regex, this should be enough to match the MasterMinds regex
			require.ErrorContains(t, err, "spec.version in body should match '^(\\s*(=||!=|>|<|>=|=>|<=|=<|~|~>|\\^)")
		})
	}
}

func TestOperatorValidSemver(t *testing.T) {
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
			op := operator(operatorsv1alpha1.OperatorSpec{
				PackageName: "package",
				Version:     d,
			})
			err := cl.Create(ctx, op)
			require.NoErrorf(t, err, "unexpected error for semver range %q: %w", d, err)
		})
	}
}

func TestOperatorInvalidChannel(t *testing.T) {
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
			err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
				PackageName: "package",
				Channel:     d,
			}))
			require.Errorf(t, err, "expected error for invalid channel %q", d)
			require.ErrorContains(t, err, "spec.channel in body should match '^[a-z0-9]+([\\.-][a-z0-9]+)*$'")
		})
	}
}

func TestOperatorValidChannel(t *testing.T) {
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
			op := operator(operatorsv1alpha1.OperatorSpec{
				PackageName: "package",
				Channel:     d,
			})
			err := cl.Create(ctx, op)
			require.NoErrorf(t, err, "unexpected error creating valid channel %q: %w", d, err)
		})
	}
}
