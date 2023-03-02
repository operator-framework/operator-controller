package bundles_and_dependencies_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/bundles_and_dependencies"
	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
)

func TestBundlesAndDeps(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BundlesAndDependenciesVariableSource Suite")
}

var _ = Describe("BundleVariable", func() {
	var (
		bv           *bundles_and_dependencies.BundleVariable
		bundleEntity *olmentity.BundleEntity
		dependencies []*olmentity.BundleEntity
	)

	BeforeEach(func() {
		bundleEntity = olmentity.NewBundleEntity(input.NewEntity("bundle-1", map[string]string{
			property.TypePackage: `{"packageName": "test-package", "version": "1.0.0"}`,
			property.TypeChannel: `{"channelName":"stable","priority":0}`,
		}))
		dependencies = []*olmentity.BundleEntity{
			olmentity.NewBundleEntity(input.NewEntity("bundle-2", map[string]string{
				property.TypePackage: `{"packageName": "test-package-2", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
			olmentity.NewBundleEntity(input.NewEntity("bundle-3", map[string]string{
				property.TypePackage: `{"packageName": "test-package-3", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
		}
		bv = bundles_and_dependencies.NewBundleVariable(bundleEntity, dependencies)
	})

	It("should return the correct bundle entity", func() {
		Expect(bv.BundleEntity()).To(Equal(bundleEntity))
	})

	It("should return the correct dependencies", func() {
		Expect(bv.Dependencies()).To(Equal(dependencies))
	})
})

var _ = Describe("BundlesAndDepsVariableSource", func() {
	var (
		bdvs             *bundles_and_dependencies.BundlesAndDepsVariableSource
		mockEntitySource input.EntitySource
	)

	BeforeEach(func() {
		bdvs = bundles_and_dependencies.NewBundlesAndDepsVariableSource(
			&MockRequiredPackageSource{
				ResultSet: []deppy.Variable{
					// must match data in mockEntitySource
					required_package.NewRequiredPackageVariable("test-package", []*olmentity.BundleEntity{
						olmentity.NewBundleEntity(input.NewEntity("bundle-2", map[string]string{
							property.TypePackage:         `{"packageName": "test-package", "version": "2.0.0"}`,
							property.TypeChannel:         `{"channelName":"stable","priority":0}`,
							property.TypeGVKRequired:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
							property.TypePackageRequired: `[{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}]`,
						})),
						olmentity.NewBundleEntity(input.NewEntity("bundle-1", map[string]string{
							property.TypePackage:     `{"packageName": "test-package", "version": "1.0.0"}`,
							property.TypeChannel:     `{"channelName":"stable","priority":0}`,
							property.TypeGVKRequired: `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
						})),
					}),
				},
			},
			&MockRequiredPackageSource{
				ResultSet: []deppy.Variable{
					// must match data in mockEntitySource
					required_package.NewRequiredPackageVariable("test-package-2", []*olmentity.BundleEntity{
						// test-package-2 required package - no dependencies
						olmentity.NewBundleEntity(input.NewEntity("bundle-15", map[string]string{
							property.TypePackage: `{"packageName": "test-package-2", "version": "1.5.0"}`,
							property.TypeChannel: `{"channelName":"stable","priority":0}`,
						})),
						olmentity.NewBundleEntity(input.NewEntity("bundle-16", map[string]string{
							property.TypePackage: `{"packageName": "test-package-2", "version": "2.0.1"}`,
							property.TypeChannel: `{"channelName":"stable","priority":0}`,
						})),
						olmentity.NewBundleEntity(input.NewEntity("bundle-17", map[string]string{
							property.TypePackage: `{"packageName": "test-package-2", "version": "3.16.0"}`,
							property.TypeChannel: `{"channelName":"stable","priority":0}`,
						})),
					}),
				},
			},
		)
		mockEntitySource = input.NewCacheQuerier(map[deppy.Identifier]input.Entity{
			// required package bundles
			"bundle-1": *input.NewEntity("bundle-1", map[string]string{
				property.TypePackage:     `{"packageName": "test-package", "version": "1.0.0"}`,
				property.TypeChannel:     `{"channelName":"stable","priority":0}`,
				property.TypeGVKRequired: `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
			}),
			"bundle-2": *input.NewEntity("bundle-2", map[string]string{
				property.TypePackage:         `{"packageName": "test-package", "version": "2.0.0"}`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
				property.TypeGVKRequired:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
				property.TypePackageRequired: `[{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}]`,
			}),

			// dependencies
			"bundle-4": *input.NewEntity("bundle-4", map[string]string{
				property.TypePackage: `{"packageName": "some-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-5": *input.NewEntity("bundle-5", map[string]string{
				property.TypePackage: `{"packageName": "some-package", "version": "1.5.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-6": *input.NewEntity("bundle-6", map[string]string{
				property.TypePackage: `{"packageName": "some-package", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-7": *input.NewEntity("bundle-7", map[string]string{
				property.TypePackage: `{"packageName": "some-other-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
				property.TypeGVK:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
			}),
			"bundle-8": *input.NewEntity("bundle-8", map[string]string{
				property.TypePackage:         `{"packageName": "some-other-package", "version": "1.5.0"}`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
				property.TypeGVK:             `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
				property.TypeGVKRequired:     `[{"group":"bar.io","kind":"Bar","version":"v1"}]`,
				property.TypePackageRequired: `[{"packageName": "another-package", "versionRange": "< 2.0.0"}]`,
			}),

			// dependencies of dependencies
			"bundle-9": *input.NewEntity("bundle-9", map[string]string{
				property.TypePackage: `{"packageName": "another-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
				property.TypeGVK:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
			}),
			"bundle-10": *input.NewEntity("bundle-10", map[string]string{
				property.TypePackage: `{"packageName": "bar-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
				property.TypeGVK:     `[{"group":"bar.io","kind":"Bar","version":"v1"}]`,
			}),
			"bundle-11": *input.NewEntity("bundle-11", map[string]string{
				property.TypePackage: `{"packageName": "bar-package", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
				property.TypeGVK:     `[{"group":"bar.io","kind":"Bar","version":"v1"}]`,
			}),

			// test-package-2 required package - no dependencies
			"bundle-15": *input.NewEntity("bundle-15", map[string]string{
				property.TypePackage: `{"packageName": "test-package-2", "version": "1.5.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-16": *input.NewEntity("bundle-16", map[string]string{
				property.TypePackage: `{"packageName": "test-package-2", "version": "2.0.1"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-17": *input.NewEntity("bundle-17", map[string]string{
				property.TypePackage: `{"packageName": "test-package-2", "version": "3.16.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),

			// completely unrelated
			"bundle-12": *input.NewEntity("bundle-12", map[string]string{
				property.TypePackage: `{"packageName": "unrelated-package", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-13": *input.NewEntity("bundle-13", map[string]string{
				property.TypePackage: `{"packageName": "unrelated-package-2", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-14": *input.NewEntity("bundle-14", map[string]string{
				property.TypePackage: `{"packageName": "unrelated-package-2", "version": "3.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
		})
	})

	It("should return bundle variables with correct dependencies", func() {
		variables, err := bdvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).NotTo(HaveOccurred())

		var bundleVariables []*bundles_and_dependencies.BundleVariable
		for _, variable := range variables {
			switch v := variable.(type) {
			case *bundles_and_dependencies.BundleVariable:
				bundleVariables = append(bundleVariables, v)
			}
		}
		Expect(len(bundleVariables)).To(Equal(12))
		Expect(bundleVariables).To(WithTransform(CollectBundleVariableIDs, Equal([]string{"bundle-2", "bundle-1", "bundle-15", "bundle-16", "bundle-17", "bundle-9", "bundle-8", "bundle-7", "bundle-5", "bundle-4", "bundle-11", "bundle-10"})))

		// check dependencies for one of the bundles
		bundle2 := VariableWithID("bundle-2")(bundleVariables)
		Expect(bundle2.Dependencies()).To(WithTransform(CollectDeppyEntities, Equal([]*input.Entity{
			input.NewEntity("bundle-9", map[string]string{
				property.TypePackage: `{"packageName": "another-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
				property.TypeGVK:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
			}),
			input.NewEntity("bundle-8", map[string]string{
				property.TypePackage:         `{"packageName": "some-other-package", "version": "1.5.0"}`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
				property.TypeGVK:             `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
				property.TypeGVKRequired:     `[{"group":"bar.io","kind":"Bar","version":"v1"}]`,
				property.TypePackageRequired: `[{"packageName": "another-package", "versionRange": "< 2.0.0"}]`,
			}),
			input.NewEntity("bundle-7", map[string]string{
				property.TypePackage: `{"packageName": "some-other-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
				property.TypeGVK:     `[{"group":"foo.io","kind":"Foo","version":"v1"}]`,
			}),
			input.NewEntity("bundle-5", map[string]string{
				property.TypePackage: `{"packageName": "some-package", "version": "1.5.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			input.NewEntity("bundle-4", map[string]string{
				property.TypePackage: `{"packageName": "some-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
		})))
	})

	It("should return error if dependencies not found", func() {
		mockEntitySource = input.NewCacheQuerier(map[deppy.Identifier]input.Entity{})
		_, err := bdvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).To(HaveOccurred())
	})
})

type MockRequiredPackageSource struct {
	ResultSet []deppy.Variable
}

func (m *MockRequiredPackageSource) GetVariables(ctx context.Context, entitySource input.EntitySource) ([]deppy.Variable, error) {
	return m.ResultSet, nil
}

func VariableWithID(id deppy.Identifier) func(vars []*bundles_and_dependencies.BundleVariable) *bundles_and_dependencies.BundleVariable {
	return func(vars []*bundles_and_dependencies.BundleVariable) *bundles_and_dependencies.BundleVariable {
		for i := 0; i < len(vars); i++ {
			if vars[i].Identifier() == id {
				return vars[i]
			}
		}
		return nil
	}
}

func CollectBundleVariableIDs(vars []*bundles_and_dependencies.BundleVariable) []string {
	var ids []string
	for _, v := range vars {
		ids = append(ids, v.Identifier().String())
	}
	return ids
}

func CollectDeppyEntities(vars []*olmentity.BundleEntity) []*input.Entity {
	var entities []*input.Entity
	for _, v := range vars {
		entities = append(entities, v.Entity)
	}
	return entities
}
