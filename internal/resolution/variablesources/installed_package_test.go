package variablesources_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/operator-framework/deppy/pkg/deppy"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	"github.com/operator-framework/operator-registry/alpha/property"

	"github.com/operator-framework/operator-controller/internal/catalogmetadata"
	olmvariables "github.com/operator-framework/operator-controller/internal/resolution/variables"
	"github.com/operator-framework/operator-controller/internal/resolution/variablesources"
	testutil "github.com/operator-framework/operator-controller/test/util"
)

var _ = Describe("InstalledPackageVariableSource", func() {
	var (
		ipvs              *variablesources.InstalledPackageVariableSource
		fakeCatalogClient testutil.FakeCatalogClient
		bundleImage       string
	)

	BeforeEach(func() {
		channel := catalogmetadata.Channel{Channel: declcfg.Channel{
			Name: "stable",
			Entries: []declcfg.ChannelEntry{
				{
					Name: "test-package.v1.0.0",
				},
				{
					Name:     "test-package.v2.0.0",
					Replaces: "test-package.v1.0.0",
				},
				{
					Name:     "test-package.v3.0.0",
					Replaces: "test-package.v2.0.0",
				},
				{
					Name:     "test-package.v4.0.0",
					Replaces: "test-package.v3.0.0",
				},
				{
					Name:     "test-package.v5.0.0",
					Replaces: "test-package.v4.0.0",
				},
			},
		}}
		bundleList := []*catalogmetadata.Bundle{
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v1.0.0",
				Package: "test-package",
				Image:   "registry.io/repo/test-package@v1.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "1.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v3.0.0",
				Package: "test-package",
				Image:   "registry.io/repo/test-package@v3.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "3.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v2.0.0",
				Package: "test-package",
				Image:   "registry.io/repo/test-package@v2.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "2.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v4.0.0",
				Package: "test-package",
				Image:   "registry.io/repo/test-package@v4.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "4.0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
			{Bundle: declcfg.Bundle{
				Name:    "test-package.v5.0.0",
				Package: "test-package",
				Image:   "registry.io/repo/test-package@v5.0.0",
				Properties: []property.Property{
					{Type: property.TypePackage, Value: json.RawMessage(`{"packageName": "test-package", "version": "5-0.0"}`)},
				}},
				InChannels: []*catalogmetadata.Channel{&channel},
			},
		}
		var err error
		bundleImage = "registry.io/repo/test-package@v2.0.0"
		fakeCatalogClient = testutil.NewFakeCatalogClient(bundleList)
		ipvs, err = variablesources.NewInstalledPackageVariableSource(&fakeCatalogClient, bundleImage)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return the correct package variable", func() {
		variables, err := ipvs.GetVariables(context.TODO())
		Expect(err).NotTo(HaveOccurred())
		Expect(variables).To(HaveLen(1))
		reqPackageVar, ok := variables[0].(*olmvariables.InstalledPackageVariable)
		Expect(ok).To(BeTrue())
		Expect(reqPackageVar.Identifier()).To(Equal(deppy.IdentifierFromString("installed package test-package")))

		// ensure bundles are in version order (high to low)
		Expect(reqPackageVar.Bundles()).To(HaveLen(2))
		Expect(reqPackageVar.Bundles()[0].Name).To(Equal("test-package.v3.0.0"))
		Expect(reqPackageVar.Bundles()[1].Name).To(Equal("test-package.v2.0.0"))
	})
})
