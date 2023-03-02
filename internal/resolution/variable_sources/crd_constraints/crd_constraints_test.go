package crd_constraints_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/bundles_and_dependencies"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/crd_constraints"
	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
)

func TestGlobalConstraints(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CRDUniquenessConstraintsVariableSource Suite")
}

var _ = Describe("BundleUniquenessVariable", func() {
	var (
		id                       deppy.Identifier
		atMostIDs                []deppy.Identifier
		globalConstraintVariable *crd_constraints.BundleUniquenessVariable
	)

	BeforeEach(func() {
		id = deppy.IdentifierFromString("test-id")
		atMostIDs = []deppy.Identifier{
			deppy.IdentifierFromString("test-at-most-id-1"),
			deppy.IdentifierFromString("test-at-most-id-2"),
		}
		globalConstraintVariable = crd_constraints.NewBundleUniquenessVariable(id, atMostIDs...)
	})

	It("should initialize a new global constraint variable", func() {
		Expect(globalConstraintVariable.Identifier()).To(Equal(id))
		Expect(globalConstraintVariable.Constraints()).To(Equal([]deppy.Constraint{constraint.AtMost(1, atMostIDs...)}))
	})
})

var bundleSet = map[deppy.Identifier]*input.Entity{
	// required package bundles
	"bundle-1": input.NewEntity("bundle-1", map[string]string{
		property.TypePackage:     `{"packageName": "test-package", "version": "1.0.0"}`,
		property.TypeChannel:     `{"channelName":"stable","priority":0}`,
		property.TypeGVKRequired: `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
		property.TypeGVK:         `[{"group":"bit.io","kind":"Bit","version":"v1"}]`,
	}),
	"bundle-2": input.NewEntity("bundle-2", map[string]string{
		property.TypePackage:         `{"packageName": "test-package", "version": "2.0.0"}`,
		property.TypeChannel:         `{"channelName":"stable","priority":0}`,
		property.TypeGVKRequired:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
		property.TypePackageRequired: `[{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}]`,
		property.TypeGVK:             `[{"group":"bit.io","kind":"Bit","version":"v1"}]`,
	}),

	// dependencies
	"bundle-3": input.NewEntity("bundle-3", map[string]string{
		property.TypePackage: `{"packageName": "some-package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`,
	}),
	"bundle-4": input.NewEntity("bundle-4", map[string]string{
		property.TypePackage: `{"packageName": "some-package", "version": "1.5.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`,
	}),
	"bundle-5": input.NewEntity("bundle-5", map[string]string{
		property.TypePackage: `{"packageName": "some-package", "version": "2.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"fiz.io","kind":"Fiz","version":"v1"}]`,
	}),
	"bundle-6": input.NewEntity("bundle-6", map[string]string{
		property.TypePackage: `{"packageName": "some-other-package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
	}),
	"bundle-7": input.NewEntity("bundle-7", map[string]string{
		property.TypePackage:         `{"packageName": "some-other-package", "version": "1.5.0"}`,
		property.TypeChannel:         `{"channelName":"stable","priority":0}`,
		property.TypeGVK:             `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
		property.TypeGVKRequired:     `[{"group":"bar.io","kind":"Bar","version":"v1"}]`,
		property.TypePackageRequired: `[{"packageName": "another-package", "versionRange": "< 2.0.0"}]`,
	}),

	// dependencies of dependencies
	"bundle-8": input.NewEntity("bundle-8", map[string]string{
		property.TypePackage: `{"packageName": "another-package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
	}),
	"bundle-9": input.NewEntity("bundle-9", map[string]string{
		property.TypePackage: `{"packageName": "bar-package", "version": "1.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"bar.io","kind":"Bar","version":"v1"}]`,
	}),
	"bundle-10": input.NewEntity("bundle-10", map[string]string{
		property.TypePackage: `{"packageName": "bar-package", "version": "2.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"bar.io","kind":"Bar","version":"v1"}]`,
	}),

	// test-package-2 required package - no dependencies
	"bundle-14": input.NewEntity("bundle-14", map[string]string{
		property.TypePackage: `{"packageName": "test-package-2", "version": "1.5.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"buz.io","kind":"Buz","version":"v1"}]`,
	}),
	"bundle-15": input.NewEntity("bundle-15", map[string]string{
		property.TypePackage: `{"packageName": "test-package-2", "version": "2.0.1"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"buz.io","kind":"Buz","version":"v1"}]`,
	}),
	"bundle-16": input.NewEntity("bundle-16", map[string]string{
		property.TypePackage: `{"packageName": "test-package-2", "version": "3.16.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"buz.io","kind":"Buz","version":"v1"}]`,
	}),

	// completely unrelated
	"bundle-11": input.NewEntity("bundle-11", map[string]string{
		property.TypePackage: `{"packageName": "unrelated-package", "version": "2.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`,
	}),
	"bundle-12": input.NewEntity("bundle-12", map[string]string{
		property.TypePackage: `{"packageName": "unrelated-package-2", "version": "2.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`,
	}),
	"bundle-13": input.NewEntity("bundle-13", map[string]string{
		property.TypePackage: `{"packageName": "unrelated-package-2", "version": "3.0.0"}`,
		property.TypeChannel: `{"channelName":"stable","priority":0}`,
		property.TypeGVK:     `[{"group":"buz.io","kind":"Buz","version":"v1alpha1"}]`,
	}),
}

var _ = Describe("CRDUniquenessConstraintsVariableSource", func() {
	var (
		inputVariableSource         *MockInputVariableSource
		crdConstraintVariableSource *crd_constraints.CRDUniquenessConstraintsVariableSource
		ctx                         context.Context
		entitySource                input.EntitySource
	)

	BeforeEach(func() {
		inputVariableSource = &MockInputVariableSource{}
		crdConstraintVariableSource = crd_constraints.NewCRDUniquenessConstraintsVariableSource(inputVariableSource)
		ctx = context.Background()

		// the entity is not used in this variable source
		entitySource = &PanicEntitySource{}
	})

	It("should get variables from the input variable source and create global constraint variables", func() {
		inputVariableSource.ResultSet = []deppy.Variable{
			required_package.NewRequiredPackageVariable("test-package", []*olmentity.BundleEntity{
				olmentity.NewBundleEntity(bundleSet["bundle-2"]),
				olmentity.NewBundleEntity(bundleSet["bundle-1"]),
			}),
			required_package.NewRequiredPackageVariable("test-package-2", []*olmentity.BundleEntity{
				olmentity.NewBundleEntity(bundleSet["bundle-14"]),
				olmentity.NewBundleEntity(bundleSet["bundle-15"]),
				olmentity.NewBundleEntity(bundleSet["bundle-16"]),
			}),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-2"]),
				[]*olmentity.BundleEntity{
					olmentity.NewBundleEntity(bundleSet["bundle-3"]),
					olmentity.NewBundleEntity(bundleSet["bundle-4"]),
					olmentity.NewBundleEntity(bundleSet["bundle-5"]),
					olmentity.NewBundleEntity(bundleSet["bundle-6"]),
					olmentity.NewBundleEntity(bundleSet["bundle-7"]),
				},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-1"]),
				[]*olmentity.BundleEntity{
					olmentity.NewBundleEntity(bundleSet["bundle-6"]),
					olmentity.NewBundleEntity(bundleSet["bundle-7"]),
					olmentity.NewBundleEntity(bundleSet["bundle-8"]),
				},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-3"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-4"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-5"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-6"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-7"]),
				[]*olmentity.BundleEntity{
					olmentity.NewBundleEntity(bundleSet["bundle-8"]),
					olmentity.NewBundleEntity(bundleSet["bundle-9"]),
					olmentity.NewBundleEntity(bundleSet["bundle-10"]),
				},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-8"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-9"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-10"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-14"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-15"]),
				[]*olmentity.BundleEntity{},
			),
			bundles_and_dependencies.NewBundleVariable(
				olmentity.NewBundleEntity(bundleSet["bundle-16"]),
				[]*olmentity.BundleEntity{},
			),
		}
		variables, err := crdConstraintVariableSource.GetVariables(ctx, entitySource)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(variables)).To(Equal(26))
		var crdConstraintVariables []*crd_constraints.BundleUniquenessVariable
		for _, variable := range variables {
			switch v := variable.(type) {
			case *crd_constraints.BundleUniquenessVariable:
				crdConstraintVariables = append(crdConstraintVariables, v)
			}
		}
		Expect(len(crdConstraintVariables)).To(Equal(11))
		Expect(crdConstraintVariables).To(WithTransform(CollectGlobalConstraintVariableIDs, ContainElements([]string{
			"another-package package uniqueness",
			"bar-package package uniqueness",
			"test-package-2 package uniqueness",
			"test-package package uniqueness",
			"some-package package uniqueness",
			"some-other-package package uniqueness",
			`group:"buz.io" version:"v1" kind:"Buz" gvk uniqueness`,
			`group:"bit.io" version:"v1" kind:"Bit" gvk uniqueness`,
			`group:"fiz.io" version:"v1" kind:"Fiz" gvk uniqueness`,
			`group:"foo.io" version:"v1" kind:"Foo" gvk uniqueness`,
			`group:"bar.io" version:"v1" kind:"Bar" gvk uniqueness`,
		})))
	})

	It("should return an error if input variable source returns an error", func() {
		inputVariableSource = &MockInputVariableSource{Err: fmt.Errorf("error getting variables")}
		crdConstraintVariableSource = crd_constraints.NewCRDUniquenessConstraintsVariableSource(inputVariableSource)
		_, err := crdConstraintVariableSource.GetVariables(ctx, entitySource)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("error getting variables"))
	})
})

var _ input.EntitySource = &PanicEntitySource{}

type PanicEntitySource struct{}

func (p PanicEntitySource) Get(ctx context.Context, id deppy.Identifier) *input.Entity {
	return nil
}

func (p PanicEntitySource) Filter(ctx context.Context, filter input.Predicate) (input.EntityList, error) {
	return nil, fmt.Errorf("if you are seeing this it is because the global variable source is calling the entity source - this shouldn't happen")
}

func (p PanicEntitySource) GroupBy(ctx context.Context, fn input.GroupByFunction) (input.EntityListMap, error) {
	return nil, fmt.Errorf("if you are seeing this it is because the global variable source is calling the entity source - this shouldn't happen")
}

func (p PanicEntitySource) Iterate(ctx context.Context, fn input.IteratorFunction) error {
	return fmt.Errorf("if you are seeing this it is because the global variable source is calling the entity source - this shouldn't happen")
}

type MockInputVariableSource struct {
	ResultSet []deppy.Variable
	Err       error
}

func (m *MockInputVariableSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.ResultSet, nil
}

func CollectGlobalConstraintVariableIDs(vars []*crd_constraints.BundleUniquenessVariable) []string {
	var ids []string
	for _, v := range vars {
		ids = append(ids, v.Identifier().String())
	}
	return ids
}
