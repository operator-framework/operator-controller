package variablesources_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/entities"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

var _ = Describe("RequiredPackageVariableSource", func() {
	var (
		rpvs             *variablesources.RequiredPackageVariableSource
		packageName      string
		mockEntitySource input.EntitySource
	)

	BeforeEach(func() {
		var err error
		packageName = "test-package"
		rpvs, err = variablesources.NewRequiredPackageVariableSource(packageName)
		Expect(err).NotTo(HaveOccurred())
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
		Expect(variables).To(HaveLen(1))
		reqPackageVar, ok := variables[0].(*olmvariables.RequiredPackageVariable)
		Expect(ok).To(BeTrue())
		Expect(reqPackageVar.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))))

		// ensure bundle entities are in version order (high to low)
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

	It("should filter by version range", func() {
		// recreate source with version range option
		var err error
		rpvs, err = variablesources.NewRequiredPackageVariableSource(packageName, variablesources.InVersionRange(">=1.0.0 !2.0.0 <3.0.0"))
		Expect(err).NotTo(HaveOccurred())

		variables, err := rpvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).NotTo(HaveOccurred())
		Expect(variables).To(HaveLen(1))
		reqPackageVar, ok := variables[0].(*olmvariables.RequiredPackageVariable)
		Expect(ok).To(BeTrue())
		Expect(reqPackageVar.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))))

		// ensure bundle entities are in version order (high to low)
		Expect(reqPackageVar.BundleEntities()).To(Equal([]*olmentity.BundleEntity{
			olmentity.NewBundleEntity(input.NewEntity("bundle-1", map[string]string{
				property.TypePackage: `{"packageName": "test-package", "version": "1.0.0"}`,
				property.TypeChannel: `{"channelName":"stable","priority":0}`,
			})),
		}))
	})

	It("should fail with bad semver range", func() {
		_, err := variablesources.NewRequiredPackageVariableSource(packageName, variablesources.InVersionRange("not a valid semver"))
		Expect(err).To(HaveOccurred())
	})

	It("should return an error if package not found", func() {
		mockEntitySource := input.NewCacheQuerier(map[deppy.Identifier]input.Entity{})
		_, err := rpvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("package 'test-package' not found"))
	})

	It("should return an error if entity source errors", func() {
		_, err := rpvs.GetVariables(context.TODO(), FailEntitySource{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("error executing filter"))
	})
})
