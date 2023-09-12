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
			Name: "test-operator",
		},
		Spec: spec,
	}
}

func TestOperatorSpecIsEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{}))
	require.Error(t, err)
	require.ErrorContains(t, err, "spec.packageName in body should match '^[a-z0-9]+(-[a-z0-9]+)*$'")
}

func TestOperatorLongPackageName(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
		PackageName: "this-is-a-really-long-package-name-that-is-greater-than-48-characters",
	}))
	require.Error(t, err)
	require.ErrorContains(t, err, "Too long: may not be longer than 48")
}

func TestOperatorLongValidSemver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
		PackageName: "package",
		Version:     "1234567890.1234567890.12345678901234567890123456789012345678901234",
	}))
	require.Error(t, err)
	require.ErrorContains(t, err, "Too long: may not be longer than 64")
}

func TestOperatorInvalidSemver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	for _, invalidSemver := range invalidSemvers {
		err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Version:     invalidSemver,
		}))
		require.Errorf(t, err, "expected error for invalid semver %q", invalidSemver)
		// Don't need to include the whole regex, this should be enough to match the MasterMinds regex
		require.ErrorContains(t, err, "spec.version in body should match '^(\\s*(=||!=|>|<|>=|=>|<=|=<|~|~>|\\^)")
	}
}

func TestOperatorValidSemver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	for _, validSemver := range validSemvers {
		op := operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Version:     validSemver,
		})
		err := cl.Create(ctx, op)
		require.NoErrorf(t, err, "unexpected error for semver range '%q': %w", validSemver, err)
		err = cl.Delete(ctx, op)
		require.NoErrorf(t, err, "unexpected error deleting valid semver '%q': %w", validSemver, err)
	}
}

func TestOperatorInvalidChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	for _, invalidChannel := range invalidChannels {
		err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Channel:     invalidChannel,
		}))
		require.Errorf(t, err, "expected error for invalid channel '%q'", invalidChannel)
		require.ErrorContains(t, err, "spec.channel in body should match '^[a-z0-9]+([\\.-][a-z0-9]+)*$'")
	}
}

func TestOperatorValidChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	validChannels := []string{
		"hyphenated-name",
		"dotted.name",
		"channel-has-version-1.0.1",
	}
	for _, validChannel := range validChannels {
		op := operator(operatorsv1alpha1.OperatorSpec{
			PackageName: "package",
			Channel:     validChannel,
		})
		err := cl.Create(ctx, op)
		require.NoErrorf(t, err, "unexpected error creating valid channel '%q': %w", validChannel, err)
		err = cl.Delete(ctx, op)
		require.NoErrorf(t, err, "unexpected error deleting valid channel '%q': %w", validChannel, err)
	}
}

func TestOperatorLongValidChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cl.Create(ctx, operator(operatorsv1alpha1.OperatorSpec{
		PackageName: "package",
		Channel:     "longname01234567890123456789012345678901234567890",
	}))

	require.Error(t, err)
	require.ErrorContains(t, err, "spec.channel: Too long: may not be longer than 48")
}
