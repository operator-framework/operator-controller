package convert_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/operator-framework/operator-controller/internal/operator-controller/rukpak/convert"
)

func Test_BundleValidatorHasAllValidationFns(t *testing.T) {
	expectedValidationFns := []func(v1 *convert.RegistryV1) []error{
		convert.CheckDeploymentSpecUniqueness,
		convert.CheckCRDResourceUniqueness,
		convert.CheckOwnedCRDExistence,
		convert.CheckPackageNameNotEmpty,
	}
	actualValidationFns := convert.RegistryV1BundleValidator

	require.Equal(t, len(expectedValidationFns), len(actualValidationFns))
	for i := range expectedValidationFns {
		require.Equal(t, reflect.ValueOf(expectedValidationFns[i]).Pointer(), reflect.ValueOf(actualValidationFns[i]).Pointer(), "bundle validator has unexpected validation function")
	}
}

func Test_BundleValidatorCallsAllValidationFnsInOrder(t *testing.T) {
	actual := ""
	validator := convert.BundleValidator{
		func(v1 *convert.RegistryV1) []error {
			actual += "h"
			return nil
		},
		func(v1 *convert.RegistryV1) []error {
			actual += "i"
			return nil
		},
	}
	require.NoError(t, validator.Validate(nil))
	require.Equal(t, "hi", actual)
}

func Test_CheckDeploymentSpecUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with unique deployment strategy spec names",
			bundle: &convert.RegistryV1{
				CSV: makeCSV(
					withStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-two"},
					),
				),
			},
			expectedErrs: []error{},
		}, {
			name: "rejects bundles with duplicate deployment strategy spec names",
			bundle: &convert.RegistryV1{
				CSV: makeCSV(
					withStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-two"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-one"},
					),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service version contains duplicate strategy deployment spec 'test-deployment-one'"),
			},
		}, {
			name: "errors are ordered by deployment strategy spec name",
			bundle: &convert.RegistryV1{
				CSV: makeCSV(
					withStrategyDeploymentSpecs(
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-a"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-b"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-c"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-b"},
						v1alpha1.StrategyDeploymentSpec{Name: "test-deployment-a"},
					),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service version contains duplicate strategy deployment spec 'test-deployment-a'"),
				errors.New("cluster service version contains duplicate strategy deployment spec 'test-deployment-b'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := convert.CheckDeploymentSpecUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CRDResourceUniqueness(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with unique custom resource definition resources",
			bundle: &convert.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{
					{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "b.crd.something"}},
				},
			},
			expectedErrs: []error{},
		}, {
			name: "rejects bundles with duplicate custom resource definition resources",
			bundle: &convert.RegistryV1{CRDs: []apiextensionsv1.CustomResourceDefinition{
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
			}},
			expectedErrs: []error{
				errors.New("bundle contains duplicate custom resource definition 'a.crd.something'"),
			},
		}, {
			name: "errors are ordered by custom resource definition name",
			bundle: &convert.RegistryV1{CRDs: []apiextensionsv1.CustomResourceDefinition{
				{ObjectMeta: metav1.ObjectMeta{Name: "c.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "c.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
			}},
			expectedErrs: []error{
				errors.New("bundle contains duplicate custom resource definition 'a.crd.something'"),
				errors.New("bundle contains duplicate custom resource definition 'c.crd.something'"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := convert.CheckCRDResourceUniqueness(tc.bundle)
			require.Equal(t, tc.expectedErrs, err)
		})
	}
}

func Test_CheckOwnedCRDExistence(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with existing owned custom resource definition resources",
			bundle: &convert.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{
					{ObjectMeta: metav1.ObjectMeta{Name: "a.crd.something"}},
					{ObjectMeta: metav1.ObjectMeta{Name: "b.crd.something"}},
				},
				CSV: makeCSV(
					withOwnedCRDs(
						v1alpha1.CRDDescription{Name: "a.crd.something"},
						v1alpha1.CRDDescription{Name: "b.crd.something"},
					),
				),
			},
			expectedErrs: []error{},
		}, {
			name: "rejects bundles with missing owned custom resource definition resources",
			bundle: &convert.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{},
				CSV: makeCSV(
					withOwnedCRDs(v1alpha1.CRDDescription{Name: "a.crd.something"}),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service definition references owned custom resource definition 'a.crd.something' not found in bundle"),
			},
		}, {
			name: "errors are ordered by owned custom resource definition name",
			bundle: &convert.RegistryV1{
				CRDs: []apiextensionsv1.CustomResourceDefinition{},
				CSV: makeCSV(
					withOwnedCRDs(
						v1alpha1.CRDDescription{Name: "a.crd.something"},
						v1alpha1.CRDDescription{Name: "c.crd.something"},
						v1alpha1.CRDDescription{Name: "b.crd.something"},
					),
				),
			},
			expectedErrs: []error{
				errors.New("cluster service definition references owned custom resource definition 'a.crd.something' not found in bundle"),
				errors.New("cluster service definition references owned custom resource definition 'b.crd.something' not found in bundle"),
				errors.New("cluster service definition references owned custom resource definition 'c.crd.something' not found in bundle"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := convert.CheckOwnedCRDExistence(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

func Test_CheckPackageNameNotEmpty(t *testing.T) {
	for _, tc := range []struct {
		name         string
		bundle       *convert.RegistryV1
		expectedErrs []error
	}{
		{
			name: "accepts bundles with non-empty package name",
			bundle: &convert.RegistryV1{
				PackageName: "not-empty",
			},
		}, {
			name:   "rejects bundles with empty package name",
			bundle: &convert.RegistryV1{},
			expectedErrs: []error{
				errors.New("package name is empty"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := convert.CheckPackageNameNotEmpty(tc.bundle)
			require.Equal(t, tc.expectedErrs, errs)
		})
	}
}

type csvOption func(version *v1alpha1.ClusterServiceVersion)

func withStrategyDeploymentSpecs(strategyDeploymentSpecs ...v1alpha1.StrategyDeploymentSpec) csvOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = strategyDeploymentSpecs
	}
}

func withOwnedCRDs(crdDesc ...v1alpha1.CRDDescription) csvOption {
	return func(csv *v1alpha1.ClusterServiceVersion) {
		csv.Spec.CustomResourceDefinitions.Owned = crdDesc
	}
}

func makeCSV(opts ...csvOption) v1alpha1.ClusterServiceVersion {
	csv := v1alpha1.ClusterServiceVersion{}
	for _, opt := range opts {
		opt(&csv)
	}
	return csv
}
