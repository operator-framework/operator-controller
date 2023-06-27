package variablesources_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

var _ = Describe("InstalledPackageVariableSource", func() {
	var (
		ipvs             *variablesources.InstalledPackageVariableSource
		bundleImage      string
		mockEntitySource input.EntitySource
	)

	BeforeEach(func() {
		var err error
		bundleImage = "registry.io/repo/test-package@v2.0.0"
		ipvs, err = variablesources.NewInstalledPackageVariableSource(bundleImage)
		Expect(err).NotTo(HaveOccurred())

		mockEntitySource = input.NewCacheQuerier(map[deppy.Identifier]input.Entity{
			"test-package.v1.0.0": *input.NewEntity("test-package.v1.0.0test-packagestable", map[string]string{
				property.TypePackage:         `{"packageName": "test-package", "version": "1.0.0"}`,
				olmentity.PropertyBundlePath: `"registry.io/repo/test-package@v1.0.0"`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
			}),
			"test-package.v3.0.0": *input.NewEntity("test-package.v3.0.0test-packagestable", map[string]string{
				property.TypePackage:         `{"packageName": "test-package", "version": "3.0.0"}`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
				olmentity.PropertyBundlePath: `"registry.io/repo/test-package@v3.0.0"`,
				"olm.replaces":               `{"replaces": "test-package.v2.0.0"}`,
			}),
			"test-package.v2.0.0": *input.NewEntity("test-package.v2.0.0test-packagestable", map[string]string{
				property.TypePackage:         `{"packageName": "test-package", "version": "2.0.0"}`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
				olmentity.PropertyBundlePath: `"registry.io/repo/test-package@v2.0.0"`,
				"olm.replaces":               `{"replaces": "test-package.v1.0.0"}`,
			}),
			"test-package.4.0.0": *input.NewEntity("test-package.v4.0.0test-packagestable", map[string]string{
				property.TypePackage:         `{"packageName": "test-package", "version": "4.0.0"}`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
				olmentity.PropertyBundlePath: `"registry.io/repo/test-package@v4.0.0"`,
				"olm.replaces":               `{"replaces": "test-package.v3.0.0"}`,
			}),
			"test-package.5.0.0": *input.NewEntity("test-package.v5.0.0test-packagestable", map[string]string{
				property.TypePackage:         `{"packageName": "test-package", "version": "5-00"}`,
				property.TypeChannel:         `{"channelName":"stable","priority":0}`,
				olmentity.PropertyBundlePath: `"registry.io/repo/test-package@v5.0.0"`,
				"olm.replaces":               `{"replaces": "test-package.v4.0.0"}`,
			}),
		})
	})

	It("should return the correct package variable", func() {
		variables, err := ipvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).NotTo(HaveOccurred())
		Expect(variables).To(HaveLen(1))
		reqPackageVar, ok := variables[0].(*olmvariables.InstalledPackageVariable)
		Expect(ok).To(BeTrue())
		Expect(reqPackageVar.Identifier()).To(Equal(deppy.IdentifierFromString("installed package test-package.v2.0.0")))

		// ensure bundle entities are in version order (high to low)
		Expect(reqPackageVar.BundleEntities()[0].ID).To(Equal(deppy.IdentifierFromString("test-package.v3.0.0test-packagestable")))
		Expect(reqPackageVar.BundleEntities()[1].ID).To(Equal(deppy.IdentifierFromString("test-package.v2.0.0test-packagestable")))
	})
})
