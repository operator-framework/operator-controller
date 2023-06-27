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

var _ = Describe("InstalledPackageVariable", func() {
	var (
		ipv            *olmvariables.InstalledPackageVariable
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
		ipv = olmvariables.NewInstalledPackageVariable(packageName, bundleEntities)
	})

	It("should return the correct package name", func() {
		Expect(ipv.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("installed package %s", packageName))))
	})

	It("should return the correct bundle entities", func() {
		Expect(ipv.BundleEntities()).To(Equal(bundleEntities))
	})
})
