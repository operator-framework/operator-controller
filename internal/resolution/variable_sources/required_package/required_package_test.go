package required_package_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
)

func TestRequiredPackage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RequiredPackageVariableSource Suite")
}

var _ = Describe("RequiredPackageVariable", func() {
	var (
		rpv            *required_package.RequiredPackageVariable
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
		rpv = required_package.NewRequiredPackageVariable(packageName, bundleEntities)
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

var _ = Describe("RequiredPackageVariableSource", func() {
	var (
		rpvs             *required_package.RequiredPackageVariableSource
		packageName      string
		mockEntitySource input.EntitySource
	)

	BeforeEach(func() {
		packageName = "test-package"
		rpvs = required_package.NewRequiredPackage(packageName)
		mockEntitySource = input.NewCacheQuerier(map[deppy.Identifier]input.Entity{
			"bundle-1": *input.NewEntity("bundle-1", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-2": *input.NewEntity("bundle-2", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "3.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-3": *input.NewEntity("bundle-3", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),

			// add some bundles from a different package
			"bundle-4": *input.NewEntity("bundle-4", map[string]string{
				property.TypePackage: `{"packageName": "test-package-2", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
			"bundle-5": *input.NewEntity("bundle-5", map[string]string{
				property.TypePackage: `{"packageName": "test-package-2", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			}),
		})
	})

	It("should return the correct package variable", func() {
		variables, err := rpvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(variables)).To(Equal(1))
		reqPackageVar, ok := variables[0].(*required_package.RequiredPackageVariable)
		Expect(ok).To(BeTrue())
		Expect(reqPackageVar.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))))

		// ensure bundle entities are in version order
		Expect(reqPackageVar.BundleEntities()).To(Equal([]*olmentity.BundleEntity{
			olmentity.NewBundleEntity(input.NewEntity("bundle-2", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "3.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
			olmentity.NewBundleEntity(input.NewEntity("bundle-3", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "2.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
			olmentity.NewBundleEntity(input.NewEntity("bundle-1", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`})),
		}))
	})

	It("should return an error if package not found", func() {
		mockEntitySource := input.NewCacheQuerier(map[deppy.Identifier]input.Entity{})
		_, err := rpvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("package 'test-package' not found"))
	})
})
