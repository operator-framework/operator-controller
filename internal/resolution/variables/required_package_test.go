package variables_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
)

var _ = Describe("RequiredPackageVariable", func() {
	var (
		rpv            *olmvariables.RequiredPackageVariable
		packageName    string
		bundleEntities []*olmentity.BundleEntity
	)

	BeforeEach(func() {
		packageName = "test-package"
		bundleEntities = []*olmentity.BundleEntity{
			olmentity.NewBundleEntity(input.NewEntity("bundle-1", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
			olmentity.NewBundleEntity(input.NewEntity("bundle-2", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
			olmentity.NewBundleEntity(input.NewEntity("bundle-3", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "3.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
		}
		rpv = olmvariables.NewRequiredPackageVariable(packageName, bundleEntities)
	})

	It("should return the correct package name", func() {
		Expect(rpv.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))))
	})

	It("should return the correct bundle entities", func() {
		Expect(rpv.BundleEntities()).To(Equal(bundleEntities))
	})

	It("should contain both mandatory and dependency constraints", func() {
		// TODO: add this test once https://github.com/operator-framework/deppy/pull/85 gets merged
		//       then we'll be able to inspect constraint types
	})
})
