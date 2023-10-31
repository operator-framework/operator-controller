package variablesources_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

func TestMakeBundleUniquenessVariables(t *testing.T) {
	const fakeCatalogName = "fake-catalog"
	channel := catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
	bundleSet := map[string]*catalogmetadata.Bundle{
		"test-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&channel},
		},
		"test-package.v1.0.1": {
			Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.1",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.1"}`)},
					{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&channel},
		},

		"some-package.v1.0.0": {
			Bundle: declcfg.Bundle{
				Name:    "some-package.v1.0.0",
				Package: "some-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.0.0"}`)},
				},
			},
			CatalogName: fakeCatalogName,
			InChannels:  []*catalogmetadata.Channel{&channel},
		},
	}

	// Input into the testable function must include more than one bundle
	// from the same package to ensure that the function
	// enforces uniqueness correctly.
	// We also need at least one bundle variable to have a dependency
	// on another package. This will help to make sure that the function
	// also creates uniqueness variables for dependencies.
	bundleVariables := []*olmvariables.BundleVariable{
		olmvariables.NewBundleVariable(
			bundleSet["test-package.v1.0.0"],
			[]*catalogmetadata.Bundle{
				bundleSet["some-package.v1.0.0"],
			},
		),
		olmvariables.NewBundleVariable(
			bundleSet["test-package.v1.0.1"],
			[]*catalogmetadata.Bundle{
				bundleSet["some-package.v1.0.0"],
			},
		),
	}

	variables := variablesources.MakeBundleUniquenessVariables(bundleVariables)

	// Each package in the input must have own uniqueness variable.
	// Each variable expected to have one constraint enforcing at most one
	// of the involved bundles to be allowed in the solution
	expectedVariables := []*olmvariables.BundleUniquenessVariable{
		{
			SimpleVariable: input.NewSimpleVariable(
				"test-package package uniqueness",
				constraint.AtMost(
					1,
					deppy.Identifier("fake-catalog-test-package-test-package.v1.0.0"),
					deppy.Identifier("fake-catalog-test-package-test-package.v1.0.1"),
				),
			),
		},
		{
			SimpleVariable: input.NewSimpleVariable(
				"some-package package uniqueness",
				constraint.AtMost(
					1,
					deppy.Identifier("fake-catalog-some-package-some-package.v1.0.0"),
				),
			),
		},
	}
	require.Empty(t, cmp.Diff(variables, expectedVariables, cmp.AllowUnexported(input.SimpleVariable{}, constraint.AtMostConstraint{})))
}

var channel = catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
var bundleSet = map[string]*catalogmetadata.Bundle{
	// required package bundles
	"bundle-1": {Bundle: declcfg.Bundle{
		Name:    "bundle-1",
		Package: "test-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
			{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bit.io","kind":"Bit","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-2": {Bundle: declcfg.Bundle{
		Name:    "bundle-2",
		Package: "test-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
			{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
			{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`{"group":"bit.io","kind":"Bit","version":"v1"}`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},

	// dependencies
	"bundle-3": {Bundle: declcfg.Bundle{
		Name:    "bundle-3",
		Package: "some-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-4": {Bundle: declcfg.Bundle{
		Name:    "bundle-4",
		Package: "some-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.5.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-5": {Bundle: declcfg.Bundle{
		Name:    "bundle-5",
		Package: "some-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "2.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-6": {Bundle: declcfg.Bundle{
		Name:    "bundle-6",
		Package: "some-other-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-7": {Bundle: declcfg.Bundle{
		Name:    "bundle-7",
		Package: "some-other-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.5.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`{"group":"foo.io","kind":"Foo","version":"v1"}`)},
			{Type: property.TypeGVKRequired, Value: json.RawMessage(`{"group":"bar.io","kind":"Bar","version":"v1"}`)},
			{Type: property.TypePackageRequired, Value: json.RawMessage(`{"packageName": "another-package", "versionRange": "< 2.0.0"}`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},

	// dependencies of dependencies
	"bundle-8": {Bundle: declcfg.Bundle{
		Name:    "bundle-8",
		Package: "another-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "another-package", "version": "1.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-9": {Bundle: declcfg.Bundle{
		Name:    "bundle-9",
		Package: "bar-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "1.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-10": {Bundle: declcfg.Bundle{
		Name:    "bundle-10",
		Package: "bar-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "2.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},

	// test-package-2 required package - no dependencies
	"bundle-14": {Bundle: declcfg.Bundle{
		Name:    "bundle-14",
		Package: "test-package-2",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.5.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-15": {Bundle: declcfg.Bundle{
		Name:    "bundle-15",
		Package: "test-package-2",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.1"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-16": {Bundle: declcfg.Bundle{
		Name:    "bundle-16",
		Package: "test-package-2",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "3.16.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},

	// completely unrelated
	"bundle-11": {Bundle: declcfg.Bundle{
		Name:    "bundle-11",
		Package: "unrelated-package",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package", "version": "2.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-12": {Bundle: declcfg.Bundle{
		Name:    "bundle-12",
		Package: "unrelated-package-2",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "2.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
	"bundle-13": {Bundle: declcfg.Bundle{
		Name:    "bundle-13",
		Package: "unrelated-package-2",
		Properties: []property.Property{
			{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "3.0.0"}`)},
			{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`)},
		}},
		InChannels: []*catalogmetadata.Channel{&channel},
	},
}

var _ = Describe("CRDUniquenessConstraintsVariableSource", func() {
	var (
		inputVariableSource         *MockInputVariableSource
		crdConstraintVariableSource *variablesources.CRDUniquenessConstraintsVariableSource
		ctx                         context.Context
	)

	BeforeEach(func() {
		inputVariableSource = &MockInputVariableSource{}
		crdConstraintVariableSource = variablesources.NewCRDUniquenessConstraintsVariableSource(inputVariableSource)
		ctx = context.Background()
	})

	It("should get variables from the input variable source and create global constraint variables", func() {
		inputVariableSource.ResultSet = []deppy.Variable{
			olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
				bundleSet["bundle-2"],
				bundleSet["bundle-1"],
			}),
			olmvariables.NewRequiredPackageVariable("test-package-2", []*catalogmetadata.Bundle{
				bundleSet["bundle-14"],
				bundleSet["bundle-15"],
				bundleSet["bundle-16"],
			}),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-2"],
				[]*catalogmetadata.Bundle{
					bundleSet["bundle-3"],
					bundleSet["bundle-4"],
					bundleSet["bundle-5"],
					bundleSet["bundle-6"],
					bundleSet["bundle-7"],
				},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-1"],
				[]*catalogmetadata.Bundle{
					bundleSet["bundle-6"],
					bundleSet["bundle-7"],
					bundleSet["bundle-8"],
				},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-3"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-4"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-5"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-6"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-7"],
				[]*catalogmetadata.Bundle{
					bundleSet["bundle-8"],
					bundleSet["bundle-9"],
					bundleSet["bundle-10"],
				},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-8"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-9"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-10"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-14"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-15"],
				[]*catalogmetadata.Bundle{},
			),
			olmvariables.NewBundleVariable(
				bundleSet["bundle-16"],
				[]*catalogmetadata.Bundle{},
			),
		}
		variables, err := crdConstraintVariableSource.GetVariables(ctx)
		Expect(err).ToNot(HaveOccurred())
		// Note: When accounting for GVK Uniqueness (which we are currently not doing), we
		// would expect to have 26 variables from the 5 unique GVKs (Bar, Bit, Buz, Fiz, Foo).
		Expect(variables).To(HaveLen(21))
		var crdConstraintVariables []*olmvariables.BundleUniquenessVariable
		for _, variable := range variables {
			switch v := variable.(type) {
			case *olmvariables.BundleUniquenessVariable:
				crdConstraintVariables = append(crdConstraintVariables, v)
			}
		}
		// Note: As above, the 5 GVKs would appear here as GVK uniqueness constraints
		// if GVK Uniqueness were being accounted for.
		Expect(crdConstraintVariables).To(WithTransform(CollectGlobalConstraintVariableIDs, Equal([]string{
			"test-package package uniqueness",
			"some-package package uniqueness",
			"some-other-package package uniqueness",
			"another-package package uniqueness",
			"bar-package package uniqueness",
			"test-package-2 package uniqueness",
		})))
	})

	It("should return an error if input variable source returns an error", func() {
		inputVariableSource = &MockInputVariableSource{Err: fmt.Errorf("error getting variables")}
		crdConstraintVariableSource = variablesources.NewCRDUniquenessConstraintsVariableSource(inputVariableSource)
		_, err := crdConstraintVariableSource.GetVariables(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("error getting variables"))
	})
})

type MockInputVariableSource struct {
	ResultSet []deppy.Variable
	Err       error
}

func (m *MockInputVariableSource) GetVariables(_ context.Context) ([]deppy.Variable, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.ResultSet, nil
}

func CollectGlobalConstraintVariableIDs(vars []*olmvariables.BundleUniquenessVariable) []string {
	ids := make([]string, 0, len(vars))
	for _, v := range vars {
		ids = append(ids, v.Identifier().String())
	}
	return ids
}
