package variablesources_test

import (
	"context"
	"encoding/json"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
	testutil "github.com/operator-framework/operator-controller/test/util"
)

var _ = Describe("BundlesAndDepsVariableSource", func() {
	var (
		bdvs              *variablesources.BundlesAndDepsVariableSource
		testBundleList    []*catalogmetadata.Bundle
		fakeCatalogClient testutil.FakeCatalogClient
	)

	BeforeEach(func() {
		channel := catalogmetadata.Channel{Channel: declcfg.Channel{Name: "stable"}}
		testBundleList = []*catalogmetadata.Bundle{
			// required package bundles
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-1",
					Package: "test-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
						{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},

			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-2",
					Package: "test-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
						{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
						{Type: property.TypePackageRequired, Value: json.RawMessage(`[{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}]`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},

			// dependencies
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-4",
					Package: "some-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.0.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-5",
					Package: "some-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "1.5.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-6",
					Package: "some-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-package", "version": "2.0.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-7",
					Package: "some-other-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.0.0"}`)},
						{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-8",
					Package: "some-other-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "some-other-package", "version": "1.5.0"}`)},
						{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
						{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
						{Type: property.TypePackageRequired, Value: json.RawMessage(`[{"packageName": "another-package", "versionRange": "< 2.0.0"}]`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},

			// dependencies of dependencies
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name: "bundle-9", Package: "another-package", Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "another-package", "version": "1.0.0"}`)},
						{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-10",
					Package: "bar-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "1.0.0"}`)},
						{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-11",
					Package: "bar-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "bar-package", "version": "2.0.0"}`)},
						{Type: property.TypeGVK, Value: json.RawMessage(`[{"group":"bar.io","kind":"Bar","version":"v1"}]`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},

			// test-package-2 required package - no dependencies
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-15",
					Package: "test-package-2",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.5.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-16",
					Package: "test-package-2",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.1"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-17",
					Package: "test-package-2",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "3.16.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},

			// completely unrelated
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-12",
					Package: "unrelated-package",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package", "version": "2.0.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-13",
					Package: "unrelated-package-2",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "2.0.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{
				CatalogName: "fake-catalog",
				Bundle: declcfg.Bundle{
					Name:    "bundle-14",
					Package: "unrelated-package-2",
					Properties: []property.Property{
						{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "unrelated-package-2", "version": "3.0.0"}`)},
					},
				},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
		}
		fakeCatalogClient = testutil.NewFakeCatalogClient(testBundleList)
		bdvs = variablesources.NewBundlesAndDepsVariableSource(
			&fakeCatalogClient,
			&MockRequiredPackageSource{
				ResultSet: []deppy.Variable{
					// must match data in fakeCatalogClient
					olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
						{
							CatalogName: "fake-catalog",
							Bundle: declcfg.Bundle{
								Name:    "bundle-2",
								Package: "test-package",
								Properties: []property.Property{
									{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
									{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
									{Type: property.TypePackageRequired, Value: json.RawMessage(`[{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}]`)},
								},
							},
							InChannels: []*catalogmetadata.Channel{&channel},
						},
						{
							CatalogName: "fake-catalog",
							Bundle: declcfg.Bundle{
								Name:    "bundle-1",
								Package: "test-package",
								Properties: []property.Property{
									{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
									{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
								},
							},
							InChannels: []*catalogmetadata.Channel{&channel},
						},
					}),
				},
			},
			&MockRequiredPackageSource{
				ResultSet: []deppy.Variable{
					// must match data in fakeCatalogClient
					olmvariables.NewRequiredPackageVariable("test-package-2", []*catalogmetadata.Bundle{
						// test-package-2 required package - no dependencies
						{
							CatalogName: "fake-catalog",
							Bundle: declcfg.Bundle{
								Name:    "bundle-15",
								Package: "test-package-2",
								Properties: []property.Property{
									{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.5.0"}`)},
								},
							},
							InChannels: []*catalogmetadata.Channel{&channel},
						},
						{
							CatalogName: "fake-catalog",
							Bundle: declcfg.Bundle{
								Name:    "bundle-16",
								Package: "test-package-2",
								Properties: []property.Property{
									{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.1"}`)},
								},
							},
							InChannels: []*catalogmetadata.Channel{&channel},
						},
						{
							CatalogName: "fake-catalog",
							Bundle: declcfg.Bundle{
								Name:    "bundle-17",
								Package: "test-package-2",
								Properties: []property.Property{
									{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "3.16.0"}`)},
								},
							},
							InChannels: []*catalogmetadata.Channel{&channel},
						},
					}),
				},
			},
		)
	})

	It("should return bundle variables with correct dependencies", func() {
		variables, err := bdvs.GetVariables(context.TODO())
		Expect(err).NotTo(HaveOccurred())

		var bundleVariables []*olmvariables.BundleVariable
		for _, variable := range variables {
			switch v := variable.(type) {
			case *olmvariables.BundleVariable:
				bundleVariables = append(bundleVariables, v)
			}
		}
		// Note: When accounting for Required GVKs (currently not in use), we would expect additional
		// dependencies (bundles 7, 8, 9, 10, 11) to appear here due to their GVKs being required by
		// some of the packages.
		Expect(bundleVariables).To(WithTransform(CollectBundleVariableIDs, Equal([]string{
			"fake-catalog-test-package-stable-bundle-2",
			"fake-catalog-test-package-stable-bundle-1",
			"fake-catalog-test-package-2-stable-bundle-15",
			"fake-catalog-test-package-2-stable-bundle-16",
			"fake-catalog-test-package-2-stable-bundle-17",
			"fake-catalog-some-package-stable-bundle-5",
			"fake-catalog-some-package-stable-bundle-4",
		})))

		// check dependencies for one of the bundles
		bundle2 := VariableWithName("bundle-2")(bundleVariables)
		// Note: As above, bundle-2 has GVK requirements satisfied by bundles 7, 8, and 9, but they
		// will not appear in this list as we are not currently taking Required GVKs into account
		Expect(bundle2.Dependencies()).To(HaveLen(2))
		Expect(bundle2.Dependencies()[0].Name).To(Equal("bundle-5"))
		Expect(bundle2.Dependencies()[1].Name).To(Equal("bundle-4"))
	})

	It("should return error if dependencies not found", func() {
		emptyCatalogClient := testutil.NewFakeCatalogClient(make([]*catalogmetadata.Bundle, 0))

		bdvs = variablesources.NewBundlesAndDepsVariableSource(
			&emptyCatalogClient,
			&MockRequiredPackageSource{
				ResultSet: []deppy.Variable{
					// must match data in fakeCatalogClient
					olmvariables.NewRequiredPackageVariable("test-package", []*catalogmetadata.Bundle{
						{
							CatalogName: "fake-catalog",
							Bundle: declcfg.Bundle{
								Name:    "bundle-2",
								Package: "test-package",
								Properties: []property.Property{
									{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
									{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
									{Type: property.TypePackageRequired, Value: json.RawMessage(`[{"packageName": "some-package", "versionRange": ">=1.0.0 <2.0.0"}]`)},
								},
							},
							InChannels: []*catalogmetadata.Channel{{Channel: declcfg.Channel{Name: "stable"}}},
						},
						{
							CatalogName: "fake-catalog",
							Bundle: declcfg.Bundle{
								Name:    "bundle-1",
								Package: "test-package",
								Properties: []property.Property{
									{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
									{Type: property.TypeGVKRequired, Value: json.RawMessage(`[{"group":"foo.io","kind":"Foo","version":"v1"}]`)},
								},
							},
							InChannels: []*catalogmetadata.Channel{{Channel: declcfg.Channel{Name: "stable"}}},
						},
					}),
				},
			},
		)
		_, err := bdvs.GetVariables(context.TODO())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not determine dependencies for bundle with id 'fake-catalog-test-package-stable-bundle-2': could not find package dependencies for bundle 'bundle-2'"))
	})

	It("should return error if an inner variable source returns an error", func() {
		bdvs = variablesources.NewBundlesAndDepsVariableSource(
			&fakeCatalogClient,
			&MockRequiredPackageSource{Error: errors.New("fake error")},
		)
		_, err := bdvs.GetVariables(context.TODO())
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("fake error"))
	})
})

type MockRequiredPackageSource struct {
	ResultSet []deppy.Variable
	Error     error
}

func (m *MockRequiredPackageSource) GetVariables(_ context.Context) ([]deppy.Variable, error) {
	return m.ResultSet, m.Error
}

func VariableWithName(name string) func(vars []*olmvariables.BundleVariable) *olmvariables.BundleVariable {
	return func(vars []*olmvariables.BundleVariable) *olmvariables.BundleVariable {
		for i := 0; i < len(vars); i++ {
			if vars[i].Bundle().Name == name {
				return vars[i]
			}
		}
		return nil
	}
}

func CollectBundleVariableIDs(vars []*olmvariables.BundleVariable) []string {
	ids := make([]string, 0, len(vars))
	for _, v := range vars {
		ids = append(ids, v.Identifier().String())
	}
	return ids
}
