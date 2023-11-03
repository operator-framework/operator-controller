package variablesources_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
)

var _ = Describe("RequiredPackageVariableSource", func() {
	var (
		rpvs        *variablesources.RequiredPackageVariableSource
		bundleList  []*catalogmetadata.Bundle
		packageName string
	)

	BeforeEach(func() {
		var err error
		packageName = "test-package"
		channel := catalogmetadata.Channel{Channel: declcfg.Channel{
			Name: "stable",
		}}
		bundleList = []*catalogmetadata.Bundle{
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v3.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v2.0.0",
				Package: "test-package",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			// add some bundles from a different package
			{Bundle: declcfg.Bundle{
				Name:    "test-package-2.v1.0.0",
				Package: "test-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "1.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "test-package-2.v2.0.0",
				Package: "test-package-2",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package-2", "version": "2.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
		}
		rpvs, err = variablesources.NewRequiredPackageVariableSource(bundleList, packageName)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return the correct package variable", func() {
		variables, err := rpvs.GetVariables(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(variables).To(HaveLen(1))
		reqPackageVar, ok := variables[0].(*olmvariables.RequiredPackageVariable)
		Expect(ok).To(BeTrue())
		Expect(reqPackageVar.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))))
		Expect(reqPackageVar.Bundles()).To(HaveLen(3))
		// ensure bundles are in version order (high to low)
		Expect(reqPackageVar.Bundles()[0].Name).To(Equal("test-package.v3.0.0"))
		Expect(reqPackageVar.Bundles()[1].Name).To(Equal("test-package.v2.0.0"))
		Expect(reqPackageVar.Bundles()[2].Name).To(Equal("test-package.v1.0.0"))
	})

	It("should filter by version range", func() {
		// recreate source with version range option
		var err error
		rpvs, err = variablesources.NewRequiredPackageVariableSource(bundleList, packageName, variablesources.InVersionRange(">=1.0.0 !=2.0.0 <3.0.0"))
		Expect(err).NotTo(HaveOccurred())

		variables, err := rpvs.GetVariables(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(variables).To(HaveLen(1))
		reqPackageVar, ok := variables[0].(*olmvariables.RequiredPackageVariable)
		Expect(ok).To(BeTrue())
		Expect(reqPackageVar.Identifier()).To(Equal(deppy.IdentifierFromString(fmt.Sprintf("required package %s", packageName))))

		Expect(reqPackageVar.Bundles()).To(HaveLen(1))
		// test-package.v1.0.0 is the only package that matches the provided filter
		Expect(reqPackageVar.Bundles()[0].Name).To(Equal("test-package.v1.0.0"))
	})

	It("should fail with bad semver range", func() {
		_, err := variablesources.NewRequiredPackageVariableSource(bundleList, packageName, variablesources.InVersionRange("not a valid semver"))
		Expect(err).To(HaveOccurred())
	})

	It("should return an error if package not found", func() {
		rpvs, err := variablesources.NewRequiredPackageVariableSource([]*catalogmetadata.Bundle{}, packageName)
		Expect(err).NotTo(HaveOccurred())
		_, err = rpvs.GetVariables(context.TODO())
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("no package 'test-package' found"))
	})
})
