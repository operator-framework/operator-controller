package entity_test

import (
	"testing"

	"github.com/blang/semver/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
)

func TestBundleEntity(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BundleEntity Suite")
}

var _ = Describe("BundleEntity", func() {
	Describe("PackageName", func() {
		It("should return the package name if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.package": "{\"packageName\":\"prometheus\",\"version\":\"0.14.0\"}",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			packageName, err := bundleEntity.PackageName()
			Expect(err).ToNot(HaveOccurred())
			Expect(packageName).To(Equal("prometheus"))
		})
		It("should return an error if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			packageName, err := bundleEntity.PackageName()
			Expect(packageName).To(Equal(""))
			Expect(err.Error()).To(Equal("error determining package for entity 'operatorhub/prometheus/0.14.0': property 'olm.package' not found"))
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.package": "badPackageNameStructure",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			packageName, err := bundleEntity.PackageName()
			Expect(packageName).To(Equal(""))
			Expect(err.Error()).To(Equal("error determining package for entity 'operatorhub/prometheus/0.14.0': property 'olm.package' ('badPackageNameStructure') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
	})

	Describe("Version", func() {
		It("should return the bundle version if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.package": "{\"packageName\":\"prometheus\",\"version\":\"0.14.0\"}",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			version, err := bundleEntity.Version()
			Expect(err).ToNot(HaveOccurred())
			Expect(*version).To(Equal(semver.MustParse("0.14.0")))
		})
		It("should return an error if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			version, err := bundleEntity.Version()
			Expect(version).To(BeNil())
			Expect(err.Error()).To(Equal("error determining package for entity 'operatorhub/prometheus/0.14.0': property 'olm.package' not found"))
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.package": "badPackageStructure",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			version, err := bundleEntity.Version()
			Expect(version).To(BeNil())
			Expect(err.Error()).To(Equal("error determining package for entity 'operatorhub/prometheus/0.14.0': property 'olm.package' ('badPackageStructure') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
		It("should return error if the version is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.package": "{\"packageName\":\"prometheus\",\"version\":\"badversion\"}",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			version, err := bundleEntity.Version()
			Expect(version).To(BeNil())
			Expect(err.Error()).To(Equal("could not parse semver (badversion) for entity 'operatorhub/prometheus/0.14.0': No Major.Minor.Patch elements found"))
		})
	})

	Describe("ProvidedGVKs", func() {
		It("should return the bundle provided gvks if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.gvk": "[{\"group\":\"foo.io\",\"kind\":\"Foo\",\"version\":\"v1\"},{\"group\":\"bar.io\",\"kind\":\"Bar\",\"version\":\"v1alpha1\"}]",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			providedGvks, err := bundleEntity.ProvidedGVKs()
			Expect(err).ToNot(HaveOccurred())
			Expect(providedGvks).To(Equal([]olmentity.GVK{
				{Group: "foo.io", Kind: "Foo", Version: "v1"},
				{Group: "bar.io", Kind: "Bar", Version: "v1alpha1"},
			}))
		})
		It("should return nil if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			providedGvks, err := bundleEntity.ProvidedGVKs()
			Expect(err).ToNot(HaveOccurred())
			Expect(providedGvks).To(BeNil())
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.gvk": "badGvkStructure",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			providedGvks, err := bundleEntity.ProvidedGVKs()
			Expect(providedGvks).To(BeNil())
			Expect(err.Error()).To(Equal("error determining bundle provided gvks for entity 'operatorhub/prometheus/0.14.0': property 'olm.gvk' ('badGvkStructure') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
	})

	Describe("RequiredGVKs", func() {
		It("should return the bundle required gvks if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.gvk.required": "[{\"group\":\"foo.io\",\"kind\":\"Foo\",\"version\":\"v1\"},{\"group\":\"bar.io\",\"kind\":\"Bar\",\"version\":\"v1alpha1\"}]",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			requiredGvks, err := bundleEntity.RequiredGVKs()
			Expect(err).ToNot(HaveOccurred())
			Expect(requiredGvks).To(Equal([]olmentity.GVKRequired{
				{Group: "foo.io", Kind: "Foo", Version: "v1"},
				{Group: "bar.io", Kind: "Bar", Version: "v1alpha1"},
			}))
		})
		It("should return nil if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			requiredGvks, err := bundleEntity.RequiredGVKs()
			Expect(err).ToNot(HaveOccurred())
			Expect(requiredGvks).To(BeNil())
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.gvk.required": "badGvkStructure",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			requiredGvks, err := bundleEntity.RequiredGVKs()
			Expect(requiredGvks).To(BeNil())
			Expect(err.Error()).To(Equal("error determining bundle required gvks for entity 'operatorhub/prometheus/0.14.0': property 'olm.gvk.required' ('badGvkStructure') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
	})

	Describe("RequiredPackages", func() {
		It("should return the bundle required packages if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.package.required": `[{"packageName": "packageA", "versionRange": ">1.0.0"}, {"packageName": "packageB", "versionRange": ">0.5.0 <0.8.6"}]`,
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			requiredPackages, err := bundleEntity.RequiredPackages()
			Expect(err).ToNot(HaveOccurred())
			Expect(requiredPackages).To(Equal([]olmentity.PackageRequired{
				{PackageRequired: property.PackageRequired{PackageName: "packageA", VersionRange: ">1.0.0"}},
				{PackageRequired: property.PackageRequired{PackageName: "packageB", VersionRange: ">0.5.0 <0.8.6"}},
			}))
		})
		It("should return nil if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			requiredPackages, err := bundleEntity.RequiredPackages()
			Expect(err).ToNot(HaveOccurred())
			Expect(requiredPackages).To(BeNil())
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.package.required": "badRequiredPackageStructure",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			requiredPackages, err := bundleEntity.RequiredPackages()
			Expect(requiredPackages).To(BeNil())
			Expect(err.Error()).To(Equal("error determining bundle required packages for entity 'operatorhub/prometheus/0.14.0': property 'olm.package.required' ('badRequiredPackageStructure') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
	})

	Describe("ChannelName", func() {
		It("should return the bundle channel name if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.channel": "{\"channelName\":\"beta\",\"priority\":0}",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			channelName, err := bundleEntity.ChannelName()
			Expect(err).ToNot(HaveOccurred())
			Expect(channelName).To(Equal("beta"))
		})
		It("should return an error if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			channelName, err := bundleEntity.ChannelName()
			Expect(channelName).To(BeEmpty())
			Expect(err.Error()).To(Equal("error determining bundle channel properties for entity 'operatorhub/prometheus/0.14.0': property 'olm.channel' not found"))
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.channel": "badChannelPropertiesStructure",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			channelName, err := bundleEntity.ChannelName()
			Expect(channelName).To(BeEmpty())
			Expect(err.Error()).To(Equal("error determining bundle channel properties for entity 'operatorhub/prometheus/0.14.0': property 'olm.channel' ('badChannelPropertiesStructure') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
	})

	Describe("ChannelProperties", func() {
		It("should return the bundle channel properties if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.channel": `{"channelName":"beta","priority":0, "replaces": "bundle.v1.0.0", "skips": ["bundle.v0.9.0", "bundle.v0.9.6"], "skipRange": ">=0.9.0 <=0.9.6"}`,
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			channelProperties, err := bundleEntity.ChannelProperties()
			Expect(err).ToNot(HaveOccurred())
			Expect(*channelProperties).To(Equal(olmentity.ChannelProperties{
				Channel: property.Channel{
					ChannelName: "beta",
					Priority:    0,
				},
				Replaces:  "bundle.v1.0.0",
				Skips:     []string{"bundle.v0.9.0", "bundle.v0.9.6"},
				SkipRange: ">=0.9.0 <=0.9.6",
			}))
		})
		It("should return an error if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			channelProperties, err := bundleEntity.ChannelProperties()
			Expect(channelProperties).To(BeNil())
			Expect(err.Error()).To(Equal("error determining bundle channel properties for entity 'operatorhub/prometheus/0.14.0': property 'olm.channel' not found"))
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.channel": "badChannelPropertiesStructure",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			channelProperties, err := bundleEntity.ChannelProperties()
			Expect(channelProperties).To(BeNil())
			Expect(err.Error()).To(Equal("error determining bundle channel properties for entity 'operatorhub/prometheus/0.14.0': property 'olm.channel' ('badChannelPropertiesStructure') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
	})

	Describe("BundlePath", func() {
		It("should return the bundle channel properties if present", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.bundle.path": `"bundle.io/path/to/bundle"`,
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			bundlePath, err := bundleEntity.BundlePath()
			Expect(err).ToNot(HaveOccurred())
			Expect(bundlePath).To(Equal("bundle.io/path/to/bundle"))
		})
		It("should return an error if the property is not found", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{})
			bundleEntity := olmentity.NewBundleEntity(entity)
			bundlePath, err := bundleEntity.BundlePath()
			Expect(bundlePath).To(BeEmpty())
			Expect(err.Error()).To(Equal("error determining bundle path for entity 'operatorhub/prometheus/0.14.0': property 'olm.bundle.path' not found"))
		})
		It("should return error if the property is malformed", func() {
			entity := input.NewEntity("operatorhub/prometheus/0.14.0", map[string]string{
				"olm.bundle.path": "badBundlePath",
			})
			bundleEntity := olmentity.NewBundleEntity(entity)
			bundlePath, err := bundleEntity.BundlePath()
			Expect(bundlePath).To(BeEmpty())
			Expect(err.Error()).To(Equal("error determining bundle path for entity 'operatorhub/prometheus/0.14.0': property 'olm.bundle.path' ('badBundlePath') could not be parsed: invalid character 'b' looking for beginning of value"))
		})
	})
})
