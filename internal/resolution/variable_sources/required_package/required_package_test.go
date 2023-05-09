package required_package_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/deppy/pkg/deppy/input"
	"github.com/operator-framework/operator-registry/alpha/property"

	olmentity "github.com/operator-framework/operator-controller/internal/resolution/variable_sources/entity"
	"github.com/operator-framework/operator-controller/internal/resolution/variable_sources/required_package"
)

func TestRequiredPackageVariable(t *testing.T) {
	RegisterTestingT(t)
	var (
		rpv            *required_package.RequiredPackageVariable
		packageName    string
		bundleEntities []*olmentity.BundleEntity
	)
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
	t.Run("ShouldReturnCorrectPackageName", func(t *testing.T) {
		Expect(rpv.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))))
	})

	t.Run("ShouldReturnTheCorrectBundleEntities", func(t *testing.T) {
		Expect(rpv.BundleEntities()).To(Equal(bundleEntities))
	})

	// TODO: add this test once https://github.com/operator-framework/deppy/pull/85 gets merged
	t.Run("ShouldContainBothMandatoryAndDependencyConstraints", func(t *testing.T) {
	})
}

func TestRequiredPackageVariableSource(t *testing.T) {
	RegisterTestingT(t)
	var (
		rpvs             *required_package.RequiredPackageVariableSource
		packageName      string
		mockEntitySource input.EntitySource
	)

	var err error
	packageName = "test-package"
	rpvs, err = required_package.NewRequiredPackage(packageName)
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

	t.Run("ShouldReturnTheCorrectPackageVariable", func(t *testing.T) {
		variables, err := rpvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(variables)).To(Equal(1))
		reqPackageVar, ok := variables[0].(*required_package.RequiredPackageVariable)
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
	t.Run("ShouldFilterByVersionRange", func(t *testing.T) {
		// recreate source with version range option
		rpvs, err := required_package.NewRequiredPackage(packageName, required_package.InVersionRange(">=1.0.0 !2.0.0 <3.0.0"))
		Expect(err).NotTo(HaveOccurred())

		variables, err := rpvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(variables)).To(Equal(1))
		reqPackageVar, ok := variables[0].(*required_package.RequiredPackageVariable)
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

	t.Run("ShouldFailWithBadSemverRange", func(t *testing.T) {
		_, err := required_package.NewRequiredPackage(packageName, required_package.InVersionRange("not a valid semver"))
		Expect(err).To(HaveOccurred())
	})

	t.Run("ShouldReturnAnErrorIfPackageNotFound", func(t *testing.T) {
		mockEntitySource := input.NewCacheQuerier(map[deppy.Identifier]input.Entity{})
		_, err := rpvs.GetVariables(context.TODO(), mockEntitySource)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("package 'test-package' not found"))
	})

}
