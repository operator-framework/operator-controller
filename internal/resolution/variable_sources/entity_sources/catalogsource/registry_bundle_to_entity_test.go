package catalogsource_test

import (
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity_sources/catalogsource"

	catalogsourceapi "github.com/operator-framework/operator-registry/pkg/api"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RegistryBundleConverter", func() {
	It("generates entity from bundle", func() {
		// When converting a bundle to an entity, package, channel, defaultChannel and bundlePath
		// must be created as special properties that are not overwritten
		// The remaining entity properties must be aggregated from the bundle ProvidedAPIs, RequiredAPIs,
		// Dependencies and Properties.
		// Properties must be preserved except for `olm.bundle.object` properties, which are irrelevant to resolution.
		// ProvidedAPIs, RequiredAPIs and Dependencies must be converted from their legacy format
		// Values in the aggregated property set must not have duplicates.
		b := &catalogsourceapi.Bundle{
			CsvName:     "test",
			PackageName: "test",
			ChannelName: "beta",
			BundlePath:  "path/to/bundle",
			Version:     "0.1.4",
			SkipRange:   "< 0.1.4",
			Replaces:    "test-operator.v0.0.1",
			Skips:       []string{"test-operator.v0.0.2", "test-operator.v0.0.3"},
			ProvidedApis: []*catalogsourceapi.GroupVersionKind{{
				Group:   "foo",
				Version: "v1",
				Kind:    "prov1",
			}, {
				Group:   "foo",
				Version: "v1",
				Kind:    "prov2",
			}},
			RequiredApis: []*catalogsourceapi.GroupVersionKind{{
				Group:   "foo",
				Version: "v1",
				Kind:    "req1",
			}, {
				Group:   "foo",
				Version: "v1",
				Kind:    "req2",
			}, {
				Group:   "foo",
				Version: "v1",
				Kind:    "req3",
			}, {
				Group:   "foo",
				Version: "v1",
				Kind:    "req4",
			}},
			Dependencies: []*catalogsourceapi.Dependency{
				// legacy constraint types
				{
					Type:  "olm.gvk",
					Value: `{"group":"foo","version":"v1","kind":"req1"}`,
				}, {
					Type:  "olm.gvk",
					Value: `{"group":"foo","version":"v1","kind":"req2"}`,
				}, {
					Type:  "olm.package",
					Value: `{"packageName":"dep1","version":"1.1.0"}`,
				}, {
					Type:  "olm.package",
					Value: `{"packageName":"dep2","version":"<1.1.0"}`,
				}},
			Properties: []*catalogsourceapi.Property{
				{
					Type:  "olm.package",
					Value: `{"packageName":"other","version":"0.1.4"}`,
				}, {
					Type:  "olm.gvk",
					Value: `{"group":"foo","version":"v1","kind":"prov1"}`,
				}, {
					Type:  "olm.gvk.required",
					Value: `{"group":"foo","version":"v1","kind":"req1"}`,
				}, {
					Type:  "olm.gvk.required",
					Value: `{"group":"foo","version":"v1","kind":"req3"}`,
				}, {
					Type:  "olm.package.required",
					Value: `{"packageName":"dep1","version":"1.1.0"}`,
				}, {
					Type:  "olm.maxOpenShiftVersion",
					Value: "4.12",
				}, {
					Type:  "olm.deprecated",
					Value: "{}",
				}, {
					//must be omitted
					Type:  "olm.bundle.object",
					Value: `{"data": "eyJraW5kIjogIkN1c3RvbVJlc291cmNlRGVmaW5pdGlvbiIsICJhcGlWZXJzaW9uIjogImFwaWV4dGVuc2lvbnMuazhzLmlvL3YxIn0="}`,
				}},
		}
		p := &catalogsourceapi.Package{DefaultChannelName: "stable"}
		entity, err := catalogsource.EntityFromBundle("test-catalog", p, b)
		Expect(err).To(BeNil())
		Expect(entity).To(Equal(&input.Entity{
			ID: "test-catalog/test/beta/0.1.4",
			Properties: map[string]string{
				"olm.package":                `{"packageName":"test","version":"0.1.4"}`,
				"olm.channel":                `{"channelName":"beta","priority":0,"replaces":"test-operator.v0.0.1","skips":["test-operator.v0.0.2","test-operator.v0.0.3"],"skipRange":"< 0.1.4"}`,
				"olm.package.defaultChannel": "stable",
				"olm.bundle.path":            "path/to/bundle",
				"olm.maxOpenShiftVersion":    "[4.12]",
				"olm.deprecated":             "[{}]",
				"olm.package.required":       `[{"packageName":"dep1","version":"1.1.0"},{"packageName":"dep2","version":"<1.1.0"}]`,
				"olm.gvk":                    `[{"group":"foo","kind":"prov1","version":"v1"},{"group":"foo","kind":"prov2","version":"v1"}]`,
				"olm.gvk.required":           `[{"group":"foo","kind":"req1","version":"v1"},{"group":"foo","kind":"req2","version":"v1"},{"group":"foo","kind":"req3","version":"v1"},{"group":"foo","kind":"req4","version":"v1"}]`,
			},
		}))
	})
})
