package variables_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/constraint"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ = Describe("BundleVariable", func() {
	var (
		bv           *olmvariables.BundleVariable
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
		bv = olmvariables.NewBundleVariable(bundleEntity, dependencies)
	})

	It("should return the correct bundle entity", func() {
		Expect(bv.BundleEntity()).To(Equal(bundleEntity))
	})

	It("should return the correct dependencies", func() {
		Expect(bv.Dependencies()).To(Equal(dependencies))
	})
})

var _ = Describe("BundleUniquenessVariable", func() {
	var (
		id                       deppy.Identifier
		atMostIDs                []deppy.Identifier
		globalConstraintVariable *olmvariables.BundleUniquenessVariable
	)

	BeforeEach(func() {
		id = deppy.IdentifierFromString("test-id")
		atMostIDs = []deppy.Identifier{
			deppy.IdentifierFromString("test-at-most-id-1"),
			deppy.IdentifierFromString("test-at-most-id-2"),
		}
		globalConstraintVariable = olmvariables.NewBundleUniquenessVariable(id, atMostIDs...)
	})

	It("should initialize a new global constraint variable", func() {
		Expect(globalConstraintVariable.Identifier()).To(Equal(id))
		Expect(globalConstraintVariable.Constraints()).To(Equal([]deppy.Constraint{constraint.AtMost(1, atMostIDs...)}))
	})
})
