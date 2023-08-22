package predicates_test

import (
	"testing"

	bsemver "github.com/blang/semver/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	"github.com/operator-framework/operator-controller/internal/resolution/util/predicates"
)

func TestPredicates(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Predicates Suite")
}

var _ = Describe("Predicates", func() {
	Describe("WithPackageName", func() {
		It("should return true when the entity has the same package name", func() {
			entity := input.NewEntity("test", map[string]string{
				property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`,
			})
			Expect(predicates.WithPackageName("mypackage")(entity)).To(BeTrue())
			Expect(predicates.WithPackageName("notmypackage")(entity)).To(BeFalse())
		})
		It("should return false when the entity does not have a package name", func() {
			entity := input.NewEntity("test", map[string]string{})
			Expect(predicates.WithPackageName("mypackage")(entity)).To(BeFalse())
		})
	})

	Describe("InSemverRange", func() {
		It("should return true when the entity has the has version in the right range", func() {
			entity := input.NewEntity("test", map[string]string{
				property.TypePackage: `{"packageName": "mypackage", "version": "1.0.0"}`,
			})
			inRange := bsemver.MustParseRange(">=1.0.0")
			notInRange := bsemver.MustParseRange(">=2.0.0")
			Expect(predicates.InSemverRange(inRange)(entity)).To(BeTrue())
			Expect(predicates.InSemverRange(notInRange)(entity)).To(BeFalse())
		})
		It("should return false when the entity does not have a version", func() {
			entity := input.NewEntity("test", map[string]string{})
			inRange := bsemver.MustParseRange(">=1.0.0")
			Expect(predicates.InSemverRange(inRange)(entity)).To(BeFalse())
		})
	})

	Describe("InChannel", func() {
		It("should return true when the entity comes from the specified channel", func() {
			entity := input.NewEntity("test", map[string]string{
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})
			Expect(predicates.InChannel("stable")(entity)).To(BeTrue())
			Expect(predicates.InChannel("unstable")(entity)).To(BeFalse())
		})
		It("should return false when the entity does not have a channel", func() {
			entity := input.NewEntity("test", map[string]string{})
			Expect(predicates.InChannel("stable")(entity)).To(BeFalse())
		})
	})

	Describe("ProvidesGVK", func() {
		It("should return true when the entity provides the specified gvk", func() {
			entity := input.NewEntity("test", map[string]string{
				property.TypeGVK: `[{"group":"foo.io","kind":"Foo","version":"v1"},{"group":"bar.io","kind":"Bar","version":"v1"}]`,
			})
			Expect(predicates.ProvidesGVK(&olmentity.GVK{
				Group:   "foo.io",
				Version: "v1",
				Kind:    "Foo",
			})(entity)).To(BeTrue())
			Expect(predicates.ProvidesGVK(&olmentity.GVK{
				Group:   "baz.io",
				Version: "v1alpha1",
				Kind:    "Baz",
			})(entity)).To(BeFalse())
		})
		It("should return false when the entity does not provide a gvk", func() {
			entity := input.NewEntity("test", map[string]string{})
			Expect(predicates.ProvidesGVK(&olmentity.GVK{
				Group:   "foo.io",
				Version: "v1",
				Kind:    "Foo",
			})(entity)).To(BeFalse())
		})
	})

	Describe("WithBundleImage", func() {
		It("should return true when the entity provides the same bundle image", func() {
			entity := input.NewEntity("test", map[string]string{
				olmentity.PropertyBundlePath: `"registry.io/repo/image@sha256:1234567890"`,
			})
			Expect(predicates.WithBundleImage("registry.io/repo/image@sha256:1234567890")(entity)).To(BeTrue())
			Expect(predicates.WithBundleImage("registry.io/repo/image@sha256:0987654321")(entity)).To(BeFalse())
		})
		It("should return false when the entity does not provide a bundle image", func() {
			entity := input.NewEntity("test", map[string]string{})
			Expect(predicates.WithBundleImage("registry.io/repo/image@sha256:1234567890")(entity)).To(BeFalse())
		})
	})

	Describe("Replaces", func() {
		It("should return true when the entity provides the replaces property", func() {
			entity := input.NewEntity("test", map[string]string{
				"olm.bundle.channelEntry": `{"replaces": "test.v0.2.0"}`,
			})
			Expect(predicates.Replaces("test.v0.2.0")(entity)).To(BeTrue())
			Expect(predicates.Replaces("test.v0.1.0")(entity)).To(BeFalse())
		})
		It("should return false when the entity does not provide a replaces property", func() {
			entity := input.NewEntity("test", map[string]string{})
			Expect(predicates.Replaces("test.v0.2.0")(entity)).To(BeFalse())
		})
		It("should return false when the replace value is not a valid json", func() {
			entity := input.NewEntity("test", map[string]string{
				"olm.bundle.channelEntry": `{"replaces"}`,
			})
			Expect(predicates.Replaces("test.v0.2.0")(entity)).To(BeFalse())
		})
	})
})
